package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net/http"
	"net/url"
	"strings"
	"time"
)

const customerInfoPath = "/api/customer/representations"
const contractPath = "/api/contracts/customer/%d?status=A,M,O,S"
const consumptionPath = `/api/v2/consumption?
customerId=:customerId
&meteringPointNo=:meteringPointNo
&meteringPointId=:meteringPointId
&resolution=:resolution
&from=:fromDate
&to=:toDate
&latestMeasurement=:latestMeasurement
&contractType=Electricity
&isDistrictHeat=false`

type CustomerInfo struct {
	Error bool
	Owner struct {
		CustomerId uint64
		FirstName  string
		LastName   string
	}
}

type MeteringPoint struct {
	MeteringPointId string
	MeteringPointNo uint64
	Address         MeteringPointAddress
	IsDistrictHeat  bool
	Resolution      string
}

type MeteringPointAddress struct {
	StreetName  string
	HouseNumber string
	HouseLetter string
	Residence   string
	PostalCode  string
	PostalCity  string
	CountryCode string
}

func (a *MeteringPointAddress) Format() string {
	return fmt.Sprintf("%s %s%s%s", a.StreetName, a.HouseNumber, a.HouseLetter, a.Residence)
}

type ActiveContract struct {
	MeteringPoint        MeteringPoint
	MeteringPointAddress MeteringPointAddress
	ProductName          string
	Is15minAvailable     bool
}

type Contracts struct {
	Active []ActiveContract
}

type UsageTime struct {
	time.Time
}

func (ut *UsageTime) UnmarshalJSON(b []byte) error {
	value := strings.Trim(string(b), `"`)
	if value == "" || value == "null" {
		return nil
	}
	t, err := time.Parse("2006-01-02T15:04:05", value)
	if err != nil {
		return err
	}
	*ut = UsageTime{t}
	return nil
}

func (ut *UsageTime) atLocation(location *time.Location) string {
	return ut.In(location).Format(time.RFC3339)
}

type Usage struct {
	MeteringPoint *MeteringPoint
	Error         bool
	Unit          string
	CostUnit      string
	Consumption   []struct {
		FromTime   UsageTime
		EnergyCost float64
		Energy     float64
	}
}

type RequestStatusError struct {
	Msg    string
	Status int
}

func (e *RequestStatusError) Error() string {
	return fmt.Sprintf("%s (status %d)", e.Msg, e.Status)
}

func buildRequest(method string, url url.URL, accessToken string) (*http.Request, error) {
	req, err := http.NewRequest("GET", url.String(), nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+accessToken)

	return req, nil
}

func getCustomerInfo(client *http.Client, baseUrl string, accessToken string) (*CustomerInfo, error) {
	if baseUrl == "" {
		return nil, errors.New("base URL cannot be empty")
	}
	if accessToken == "" {
		return nil, &RequestStatusError{Msg: "accessToken cannot be empty", Status: 401}
	}

	customerInfoUrl, err := url.Parse(baseUrl + customerInfoPath)
	if err != nil {
		return nil, fmt.Errorf("invalid url: %s", err.Error())
	}

	if cli.Debug {
		log.Printf("get customer information: %s", customerInfoUrl.String())
	}

	req, err := buildRequest("GET", *customerInfoUrl, accessToken)
	if err != nil {
		return nil, err
	}

	// GET customer information
	res, err := client.Do(req)
	if err != nil {
		return nil, &RequestStatusError{Msg: err.Error(), Status: res.StatusCode}
	}
	defer res.Body.Close()

	if res.StatusCode != 200 {
		return nil, &RequestStatusError{Msg: "invalid response status", Status: res.StatusCode}
	}

	// Parse customer information response
	if cli.Debug {
		log.Printf("parse customer information response")
	}
	var customerInfo CustomerInfo
	err = json.NewDecoder(res.Body).Decode(&customerInfo)
	if err != nil {
		return nil, errors.Join(errors.New("unable to get parse customer information response"), err)
	}

	if customerInfo.Error {
		return nil, errors.New("invalid customer info response")
	}
	return &customerInfo, nil
}

func getMeteringPoints(client *http.Client, baseUrl string, accessToken string, customerId uint64) (*[]MeteringPoint, error) {
	if baseUrl == "" {
		return nil, errors.New("base URL cannot be empty")
	}
	contractUrl, err := url.Parse(baseUrl + fmt.Sprintf(contractPath, customerId))
	if err != nil {
		return nil, fmt.Errorf("invalid url: %s", err.Error())
	}
	req, err := buildRequest("GET", *contractUrl, accessToken)
	if err != nil {
		return nil, err
	}
	if cli.Debug {
		log.Printf("get contract information: %s", req.URL.String())
	}

	// GET invoice information
	res, err := client.Do(req)
	if err != nil {
		return nil, &RequestStatusError{Msg: err.Error(), Status: res.StatusCode}
	}
	defer res.Body.Close()

	if res.StatusCode != 200 {
		return nil, &RequestStatusError{Msg: "invalid response status", Status: res.StatusCode}
	}

	// Parse invoice information response
	if cli.Debug {
		log.Print("parse contract response")
	}

	var contractsResponse struct {
		Error     bool
		Contracts Contracts
	}
	err = json.NewDecoder(res.Body).Decode(&contractsResponse)
	if err != nil {
		return nil, errors.Join(errors.New("unable to get parse contract response"), err)
	}

	if contractsResponse.Error {
		return nil, errors.New("invalid invoice info response")
	}

	var points []MeteringPoint
	for _, active := range contractsResponse.Contracts.Active {
		if active.MeteringPoint.IsDistrictHeat {
			continue
		}
		if cli.Debug {
			log.Printf("found active contract: %#v", active)
		}
		active.MeteringPoint.Address = active.MeteringPointAddress
		if active.Is15minAvailable {
			active.MeteringPoint.Resolution = "minute"
		} else {
			active.MeteringPoint.Resolution = "hour"
		}
		points = append(points, active.MeteringPoint)
	}

	return &points, nil
}

func getConsumptionData(client *http.Client, baseUrl string, accessToken string, customerId uint64, from *time.Time, to *time.Time, meteringPoints *[]MeteringPoint) (*[]Usage, error) {
	if baseUrl == "" {
		return nil, errors.New("base URL cannot be empty")
	}

	consumptionUrl, err := url.Parse(baseUrl + strings.ReplaceAll(consumptionPath, "\n", ""))
	if err != nil {
		return nil, fmt.Errorf("invalid url: %s", err.Error())
	}
	req, err := buildRequest("GET", *consumptionUrl, accessToken)
	if err != nil {
		return nil, err
	}

	if cli.Debug {
		log.Printf("process %d metering points", len(*meteringPoints))
	}
	usage := []Usage{}

	for _, meteringPoint := range *meteringPoints {
		u, err := getMeteringPointUsage(client, req, customerId, from, to, meteringPoint)
		if err != nil {
			return nil, errors.Join(fmt.Errorf("could not get usage for meteringPointId %s", meteringPoint.MeteringPointId), err)
		}
		if u != nil {
			usage = append(usage, *u)
		}
	}

	return &usage, nil
}

func getMeteringPointUsage(client *http.Client, req *http.Request, customerId uint64, from *time.Time, to *time.Time, meteringPoint MeteringPoint) (*Usage, error) {
	if cli.Debug {
		log.Printf("get consumption for meteringPointNo=%d meteringPointId=%s", meteringPoint.MeteringPointNo, meteringPoint.MeteringPointId)
	}

	var now, then time.Time
	if to != nil {
		now = *to
	} else {
		now = time.Now().Add(time.Duration(time.Hour * 24))
	}
	if from != nil {
		then = *from
	} else {
		then = now.Add(-time.Duration(time.Hour * 24 * 3))
	}

	values := req.URL.Query()
	values.Set("customerId", fmt.Sprint(customerId))
	values.Set("meteringPointNo", fmt.Sprint(meteringPoint.MeteringPointNo))
	values.Set("meteringPointId", meteringPoint.MeteringPointId)
	values.Set("resolution", meteringPoint.Resolution)
	values.Set("from", then.Format("2006-01-02"))
	values.Set("to", now.Format("2006-01-02"))
	values.Set("latestMeasurement", now.Format(time.RFC3339))
	req.URL.RawQuery = values.Encode()
	if cli.Debug {
		log.Printf("get consumption: %s", req.URL.String())
	}

	// Get usage
	res, err := client.Do(req)
	if err != nil {
		return nil, &RequestStatusError{Msg: err.Error(), Status: getStatusCode(res, 500)}
	}
	defer res.Body.Close()

	if res.StatusCode != 200 {
		return nil, &RequestStatusError{Msg: fmt.Sprintf("invalid response status for meteringPointId %s", meteringPoint.MeteringPointId), Status: res.StatusCode}
	}

	var usageResponse Usage
	err = json.NewDecoder(res.Body).Decode(&usageResponse)
	if err != nil {
		return nil, errors.Join(fmt.Errorf("unable to parse usage response for meteringPointId %s", meteringPoint.MeteringPointId), err)
	}

	if cli.Debug {
		log.Printf("received %d consumption items for %s (%s)", len(usageResponse.Consumption), meteringPoint.MeteringPointId, meteringPoint.Address.Format())
	}
	usageResponse.MeteringPoint = &meteringPoint
	return &usageResponse, nil
}

func getStatusCode(res *http.Response, defaultValue int) int {
	if res == nil {
		return defaultValue
	}
	return res.StatusCode
}

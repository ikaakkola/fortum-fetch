package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// https://www.fortum.com/fi/sahkoa/api/auth/session
const customerInfoPath = "/api/auth/session"

//const contractPath = "/api/contracts/customer/%d?status=A,M,O,S"

// https://www.fortum.com/fi/sahkoa/api/trpc/loggedIn.timeSeries.consumptionForComparison?batch=1&input=%7B%220%22%3A%7B%22json%22%3A%7B%22meteringPointNo%22%3A%5B%22690193787%22%5D%2C%22fromDate%22%3A%222020-04-16T08%3A03%3A58.022%2B03%3A00%22%2C%22toDate%22%3A%222026-04-16T08%3A03%3A58.022%2B03%3A00%22%2C%22type%22%3A%22CONSUMPTION%22%7D%7D%7D
// https://www.fortum.com/fi/sahkoa/api/trpc/loggedIn.timeSeries.listTimeSeries?batch=1&input=%7B%220%22%3A%7B%22json%22%3A%7B%22meteringPointNo%22%3A%5B%22690193787%22%5D%2C%22fromDate%22%3A%222026-03-31T21%3A00%3A00.000Z%22%2C%22toDate%22%3A%222026-04-30T20%3A59%3A59.999Z%22%2C%22resolution%22%3A%22DAY%22%2C%22type%22%3A%22CONSUMPTION%22%7D%7D%7D

type ConsumptionInput struct {
	MeteringPointNo []string `json:"meteringPointNo"`
	FromDate        string   `json:"fromDate"`
	ToDate          string   `json:"toDate"`
	Resolution      string   `json:"resolution"`
	Type            string   `json:"type"`
}

const consumptionPath = `/api/trpc/loggedIn.timeSeries.listTimeSeries?
batch=1
&input=:input
`

type CustomerInfo struct {
	User struct {
		Email         string
		CustomerId    string
		DeliverySites []DeliverySite
	}
}

type DeliverySite struct {
	PriceArea   string
	Consumption struct {
		MeteringPointId string
		MeteringPointNo string
		PriceArea       string
		HouseType       string
		MeasurementType string
		MeteringType    string
		ContractNo      uint64
	}
	Address struct {
		StreetName  string
		HouseNumber string
		HouseLetter string
		Residence   string
		CityName    string
		ZipCode     string
	}
}

func (a *DeliverySite) FormatAddress() string {
	return fmt.Sprintf("%s %s%s%s", a.Address.StreetName, a.Address.HouseNumber, a.Address.HouseLetter, a.Address.Residence)
}

func (a *DeliverySite) MeasurementResolution() string {
	switch a.Consumption.MeasurementType {
	case "PER_15_MIN":
		return "PER_15_MIN"
	case "HOURLY":
		return "HOUR"
	case "DAILY":
		return "DAY"
	default:
		return "HOUR"
	}
}

type UsageTime struct {
	string
}

func (ut *UsageTime) UnmarshalJSON(b []byte) error {
	value := strings.Trim(string(b), `"`)
	*ut = UsageTime{value}
	return nil
}

func (ut *UsageTime) atLocation(location *time.Location) (*string, error) {
	t, err := time.ParseInLocation("2006-01-02T15:04:05.000Z", ut.string, location)
	if err != nil {
		return nil, err
	}
	v := t.In(time.UTC).Format(time.RFC3339)
	return &v, nil
}

type Usage struct {
	DeliverySite *DeliverySite
	Result       struct {
		Data struct {
			Json []struct {
				DeliverySiteCategory string
				MeasurementUnit      string
				MeteringPointNo      string
				PriceUnit            string
				CostUnit             string
				TemperatureUnit      string
				Series               []UsageSeries
			}
		}
	}
	Error struct {
		Json struct {
			Message string
			Code    uint64
			Data    struct {
				Code       string
				HttpStatus uint16
				Path       string
			}
		}
	}
}

type UsageSeries struct {
	AtUTC  UsageTime
	Energy []struct {
		Value float64
		Type  string
	}
	Cost []struct {
		Total float64
		Value float64
		Type  string
	}
	Price struct {
		Total         float64
		Value         float64
		VatAmount     float64
		VatPercentage float64
	}
	TemperatureReading struct {
		Temperature float64
	}
}

func (u *Usage) Series() ([]UsageSeries, error) {
	if len(u.Result.Data.Json) == 0 {
		return nil, errors.New("Empty Result JSON data")
	}
	if len(u.Result.Data.Json[0].Series) == 0 {
		return nil, errors.New("No data series in Result JSON data")
	}
	return u.Result.Data.Json[0].Series, nil
}

func (u *UsageSeries) TotalElectricityCost() float64 {
	for _, cost := range u.Cost {
		if cost.Type == "COST_SALES_ELECTRICITY" {
			return cost.Total
		}
	}
	return 0.0
}

func (u *UsageSeries) EnergyConsumptionValue() float64 {
	for _, energy := range u.Energy {
		if energy.Type == "ENERGY" {
			return energy.Value
		}
	}
	return 0.0
}

type RequestStatusError struct {
	Msg    string
	Status int
}

func (e *RequestStatusError) Error() string {
	return fmt.Sprintf("%s (status %d)", e.Msg, e.Status)
}

func buildRequest(method string, url url.URL, cookiesStr string) (*http.Request, error) {
	req, err := http.NewRequest(method, url.String(), nil)
	if err != nil {
		return nil, err
	}

	cookies, err := http.ParseCookie(cookiesStr)
	for _, cookie := range cookies {
		req.AddCookie(cookie)
	}

	return req, nil
}

func getCustomerInfo(client *http.Client, baseUrl string, cookies string) (*CustomerInfo, error) {
	if baseUrl == "" {
		return nil, errors.New("base URL cannot be empty")
	}
	if cookies == "" {
		return nil, &RequestStatusError{Msg: "Cookies cannot be empty", Status: 401}
	}

	customerInfoUrl, err := url.Parse(baseUrl + customerInfoPath)
	if err != nil {
		return nil, fmt.Errorf("invalid url: %s", err.Error())
	}

	if cli.Debug {
		log.Printf("get customer information: %s", customerInfoUrl.String())
	}

	req, err := buildRequest("GET", *customerInfoUrl, cookies)
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

	if customerInfo.User.CustomerId == "" {
		return nil, errors.New("invalid customer info response (no customerId received)")
	}
	if len(customerInfo.User.DeliverySites) == 0 {
		return nil, errors.New("invalid customer info response (no deliverySites received)")
	}
	return &customerInfo, nil
}

func getConsumptionData(client *http.Client, baseUrl string, cookies string, customerId string, from *time.Time, to *time.Time, meteringPoints *[]DeliverySite) (*[]Usage, error) {
	if baseUrl == "" {
		return nil, errors.New("base URL cannot be empty")
	}

	consumptionUrl, err := url.Parse(baseUrl + strings.ReplaceAll(consumptionPath, "\n", ""))
	if err != nil {
		return nil, fmt.Errorf("invalid url: %s", err.Error())
	}
	req, err := buildRequest("GET", *consumptionUrl, cookies)
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
			return nil, errors.Join(fmt.Errorf("could not get usage for meteringPointId %s", meteringPoint.Consumption.MeteringPointId), err)
		}
		if u != nil {
			usage = append(usage, *u)
		}
	}

	return &usage, nil
}

func getMeteringPointUsage(client *http.Client, req *http.Request, customerId string, from *time.Time, to *time.Time, deliverySite DeliverySite) (*Usage, error) {
	if cli.Debug {
		log.Printf("get consumption for meteringPointNo=%s meteringPointId=%s", deliverySite.Consumption.MeteringPointNo, deliverySite.Consumption.MeteringPointId)
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

	consumptionInput := ConsumptionInput{
		MeteringPointNo: []string{deliverySite.Consumption.MeteringPointNo},
		FromDate:        then.Format(time.RFC3339),
		ToDate:          now.Format(time.RFC3339),
		Resolution:      deliverySite.MeasurementResolution(),
		Type:            "CONSUMPTION",
	}

	consumptionInputJson, err := json.Marshal(consumptionInput)
	if err != nil {
		return nil, &RequestStatusError{Msg: err.Error(), Status: 400}
	}

	values := req.URL.Query()
	values.Set("input", fmt.Sprintf(`{"0":{"json":%v}}`, string(consumptionInputJson)))
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
		bodyBytes, _ := io.ReadAll(res.Body)
		return nil, &RequestStatusError{Msg: fmt.Sprintf("invalid response status for meteringPointId %s: %s", deliverySite.Consumption.MeteringPointId, string(bodyBytes)), Status: res.StatusCode}
	}

	var usageResponse []Usage
	err = json.NewDecoder(res.Body).Decode(&usageResponse)
	if err != nil {
		return nil, errors.Join(fmt.Errorf("unable to parse usage response for meteringPointId %s", deliverySite.Consumption.MeteringPointId), err)
	}

	if len(usageResponse) == 0 {
		return nil, fmt.Errorf("received no usage for meteringPointId %s", deliverySite.Consumption.MeteringPointId)
	}

	if len(usageResponse) > 1 {
		return nil, fmt.Errorf("received more than 1 usage for meteringPointId %s", deliverySite.Consumption.MeteringPointId)
	}

	usage := usageResponse[0]

	if cli.Debug {
		log.Printf("received %d consumption items for %s (%s)", len(usage.Result.Data.Json[0].Series), deliverySite.Consumption.MeteringPointId, deliverySite.FormatAddress())
	}
	usage.DeliverySite = &deliverySite
	return &usage, nil
}

func getStatusCode(res *http.Response, defaultValue int) int {
	if res == nil {
		return defaultValue
	}
	return res.StatusCode
}

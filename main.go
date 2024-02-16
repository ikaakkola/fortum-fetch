package main

import (
	"errors"
	"fmt"
	"log"
	"net/http"
	"os"
	"text/template"
	"time"

	"github.com/alecthomas/kong"
	kongyaml "github.com/alecthomas/kong-yaml"
)

var cli struct {
	Debug        bool   `help:"Enable debug logging"`
	Headless     bool   `help:"Enable headless mode" default:"true"`
	User         string `short:"u" env:"FORTUM_USER" help:"My Fortum user to authenticate as"`
	Password     string `short:"p" env:"FORTUM_PASSWORD" help:"Password for My Fortum user"`
	Url          string `env:"FORTUM_URL" help:"My Fortum URL" default:"https://web.fortum.fi"`
	Authenticate struct {
	} `cmd:"" help:"Authenticate to My Fortum and acquire access token"`

	Usage struct {
		AccessToken     string     `short:"t" env:"FORTUM_ACCESS_TOKEN" help:"Access Token"`
		MeteringPointId string     `env:"FORUTM_METERING_POINT_ID" help:"Only include data for this metering point ID"`
		TimeZone        string     `env:"FORTUM_TZ" help:"Timezone for consumption data as received from Fortum. Output is always UTC" default:"Europe/Helsinki"`
		MeteringFormat  string     `env:"FORTUM_OUTPUT_TEMPLATE" help:"Template for metering point output. Available fields: CustomerId, MeteringPointId, MeteringPointNo, MeteringPointAddress, Time, Energy, Cost" default:"${default_metering_format}"`
		From            *time.Time `optional:"" help:"Load data from this date"`
		To              *time.Time `optional:"" help:"Load data to this date"`
	} `cmd:"" help:"Get usage data"`
}

func main() {
	ctx := kong.Parse(&cli,
		kong.Name("fortum-fetch"),
		kong.Description("Utility to fetch usage data from My Fortum"),
		kong.UsageOnError(),
		kong.Configuration(kongyaml.Loader, "~/.config/fortum_fetch", "~/.fortum_fetch"),
		kong.ConfigureHelp(kong.HelpOptions{
			Compact: true,
			Summary: true,
		}),
		kong.Vars{
			"default_metering_format": "" +
				"{{.Time}},{{.CustomerId}}_{{.MeteringPointId}}_energy,{{.MeteringPointAddress}},customer {{.CustomerId}} energy for {{.MeteringPointId}} at {{.MeteringPointAddress}},{{.Energy}},kWh\n" +
				"{{.Time}},{{.CustomerId}}_{{.MeteringPointId}}_cost,{{.MeteringPointAddress}},customer {{.CustomerId}} cost for {{.MeteringPointId}} at {{.MeteringPointAddress}},{{.Cost}},€\n",
		},
	)

	switch ctx.Command() {
	case "authenticate":
		fmt.Println(doAuthenticate())
	case "usage":
		doUsage()
	}
}

func doAuthenticate() string {
	auth, err := NewAuth(&cli.User, &cli.Password, &cli.Url)
	if err != nil {
		log.Fatalf("invalid authentication configuration: %s", err.Error())
	}
	token, err := getAccessToken(auth)
	if err != nil {
		log.Fatal(err)
	}
	return *token
}

func doUsage() {
	client :=
		httpClient()

	var statusError *RequestStatusError
	customerInfo, err := getCustomerInfo(client, cli.Url, cli.Usage.AccessToken)
	if err != nil {
		switch {
		case errors.As(err, &statusError):
			if statusError.Status == 401 {
				// Try to authenticate again
				if cli.Debug {
					log.Print("authentication failure, attempting to acquire new access token")
				}
				cli.Usage.AccessToken = doAuthenticate()

				customerInfo, err = getCustomerInfo(client, cli.Url, cli.Usage.AccessToken)
				if err != nil {
					log.Fatal(err)
				}
			}
		default:
			log.Fatal(err)
		}
	}
	if cli.Debug {
		log.Printf("get metering points")
	}
	meteringPoints, err := getMeteringPoints(client, cli.Url, cli.Usage.AccessToken, customerInfo.Owner.CustomerId)
	if err != nil {
		log.Fatal(err)
	}

	if cli.Debug {
		log.Printf("metering points: %#v", meteringPoints)
	}

	if len(cli.Usage.MeteringPointId) > 0 {
		filtered := []MeteringPoint{}
		for _, item := range *meteringPoints {
			if item.MeteringPointId == cli.Usage.MeteringPointId {
				filtered = append(filtered, item)
			}
		}
		meteringPoints = &filtered
	}
	if len(*meteringPoints) == 0 {
		if cli.Debug {
			log.Print("no metering points found")
		}
		return
	}

	consumptionData, err := getConsumptionData(client, cli.Url, cli.Usage.AccessToken, customerInfo.Owner.CustomerId, cli.Usage.From, cli.Usage.To, meteringPoints)
	if err != nil {
		log.Fatal(err)
	}
	if len(*consumptionData) == 0 {
		if cli.Debug {
			log.Printf("no consumption data found")
		}
		return
	}

	if cli.Debug {
		log.Printf("metering output format: %s", cli.Usage.MeteringFormat)
	}
	meteringTemplate, err := template.New("metering").Parse(cli.Usage.MeteringFormat)
	if err != nil {
		log.Fatal(err)
	}

	type meteringRow struct {
		Time                 string
		CustomerId           uint64
		MeteringPointId      string
		MeteringPointNo      uint64
		MeteringPointAddress string
		Energy               float64
		Cost                 float64
	}

	tz, err := time.LoadLocation(cli.Usage.TimeZone)
	if err != nil {
		log.Fatal(err)
	}
	if cli.Debug {
		log.Printf("processing %d consumption items", len(*consumptionData))
	}

	for idx, item := range *consumptionData {
		if cli.Debug {
			log.Printf("processing %d metering rows for consumption item %d", len(item.Consumption), idx)
		}
		for _, ci := range item.Consumption {
			time, err := ci.FromTime.atLocation(tz)
			if err != nil {
				log.Fatal(err)
			}

			row := meteringRow{
				*time,
				customerInfo.Owner.CustomerId,
				item.MeteringPoint.MeteringPointId,
				item.MeteringPoint.MeteringPointNo,
				item.MeteringPoint.Address.Format(),
				ci.Energy,
				ci.EnergyCost,
			}

			err = meteringTemplate.Execute(os.Stdout, row)
			if err != nil {
				log.Fatal(err)
			}
		}

	}
}

func httpClient() *http.Client {
	return &http.Client{Timeout: 10 * time.Second}
}

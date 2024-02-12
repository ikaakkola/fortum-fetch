# fortum-fetch

Utility to fetch Fortum Energy consumption data from 'My Fortum'

## Requirements

* go >= 1.21.0
* chromium (chrome)

## Building

    git clone https://github.com/ikaakkola/fortum-fetch.git
    cd fortum-fetch
    go build
    
Optionally symlink the generated binary `fortum-fetch` to somewhere in $PATH

## Usage

Run `fortum-fetch -h` for usage information

## Configuration via .config/fortum-fetch

Configuration values are read from `~/.config/fortum_fetch` which is treated as YAML

For example to persist login details and :

    # username for My Fortum
    user: "hello@example.com"
    # password for My Fortum
    password: "super secret password"
    
    usage:
      # only process a single metering point
      metering-point-id: "12345678901234567890"
      
      # set the source time zone (timezone for values visible in My Fortum)
      tz: "Europe/Helsinki"
      
## Output format

The output format can be configured with a text template. The template is called once for each row of data.

The template has the following fields available

* Time
* CustomerId
* MeteringPointId
* MeteringPointNo
* MeteringPointAddress
* Energy
* Cost

For example to print each row with only the time and energy values:

    {{.Time}} {{.Energy}}\n
    

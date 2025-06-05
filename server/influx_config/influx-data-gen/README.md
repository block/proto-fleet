# Influx-data-gen
A command-line tool for generating synthetic time-series data and writing it to InfluxDB 3-core.

## Features
- Configurable sampling interval, data volume, and tags

## Prerequisites
- Go 1.18+ installed
- Access to an InfluxDB 3.x-core instance

## Running
It is advised to just run this for the influx-data-gen directory.
```bash
go run ./ --duration '100h' --interval '5s'
```

## Configuration
Configure the generator using command-line flags:

| Flag          | Default                   | Description                                    |
|---------------|---------------------------|------------------------------------------------|
| `--influx-url`| `http://localhost:8181`   | InfluxDB server URL                            |
| `--token`     | _required_                | InfluxDB authentication token                  |
| `--org`       | `my-org`                  | InfluxDB organization name                     |
| `--bucket`    | `my-bucket`               | InfluxDB bucket to write data                  |
| `--interval`  | `1s`                      | Time between writes (duration string)          |

## Notes
* Current Data generated is for a random cpu formate, it is not intended to reflect anything For fleet at this time. But provides a simple example of writing to and accessing aggregate data. We will update it to reflect miners

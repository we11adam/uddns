# UDDNS
UDDNS - Universal (or Ultimate) Dynamic DNS updater

## How to use
### Obtaining the program
You can either:

`go install github.com/we11adam/uddns@latest`

or download the binary for you platform directly from the [releases page](https://github.com/we11adam/uddns/releases/)


### Configuration file
Ceate a `uddns.yaml` file as one the following: `./uddns.yaml`, `~/.config/uddns.yaml`, 
`/etc/uddns.yaml`. UDDNS will try to read them in order. Additionally, you can specify the configuration file path with the `UDDNS_CONFIG` environment variable.
The file should look like this:

```yaml
providers:
  routeros:
    endpoint: https://192.168.88.1 # RouterOS API endpoint
    username: admin # RouterOS user with API access
    password: "" # RouterOS user password
  ip_service:
    - ifconfig.me # External service to get IP address

updaters:
  cloudflare:
    email: "user@exmaple.com" # Cloudflare account email
    apikey: fd25bdc03a4c17450aa4327aa37de4573270f # Cloudflare API key
    domain: ddns.yourdomain.com # Domain to update

notifiers:
  telegram:
    chat_id: -1001234567890 # Telegram chat ID
    token: 1234567890:E2AvwaQsEvkACAF9pVPZAICmbXuzzHFTyyv # Telegram bot token
    proxy: http://127.0.0.1:2080
```

Where:
- `providers` is a list of providers that UDDNS can use to obtain the current public IP address. Currently supported providers are:
  - `routeros`: Get IP address from a Mikrotik RouterOS device
    - `endpoint`: The RouterOS API endpoint
    - `username`: The RouterOS user with API access
    - `password`: The RouterOS user password
  - `ip_service`: Get IP address from an external service
    - `ifconfig.me`
    - `ip.sb`
    - `3322.org`
  - `netif`: Get IP address from a network interface (not implemented for Windows yet)
    - `name`: The network interface name to get the IP address from
- `updaters` is a list of updaters that UDDNS can use to update the DNS records. Currently supported updaters are:
  - `cloudflare`:
    - `email`: Cloudflare account email
    - `apikey`: Cloudflare API key
    - `domain`: Domain to update, e.g. `ddns.yourdomain.com`
  - `duckdns`:
    - `token`: DuckDNS token
    - `domain`: Domain to update, excluding the `duckdns.org` part.
  - `ddnsfm`:
    - `key`: DDNS.FM DDNS key
    - `domain`: Domain to update, e.g. `your-domain.dyn.la`
- `notifiers` is a list of notifiers that UDDNS can use to notify the user of the IP address change. Currently supported notifiers are:
  - `telegram`:
    - `token`: Telegram bot token
    - `chat_id`: Telegram chat ID
    - `proxy`: Proxy URL to use for Telegram API requests if you are behind a (great) firewall. Optional.

### Running
Run the binary as the following. It will update the DNS record with the current public IP address with a default interval of 30 seconds, which can be overriden with the `UDDNS_INTERVAL` environment variable. The format for specifying the interval is flexible, allowing values such as `60s`, `5m`, `1h`, etc.
```shell
nohup ./uddns > uddns.log 2>&1 &
```


## Roadmap
- [ ] Add more providers
- [ ] Add more updaters
- [ ] Add granular configuration options
- [ ] Add sensible logging
- [ ] Add tests
- [ ] Add CI/CD
- [ ] Add Dockerfile
- [ ] Add Daemon mode
- [ ] Add systemd service file



## Contributing
Pull requests are very welcome! For major changes, please open an issue first to discuss what you would like to change.

## License
MIT



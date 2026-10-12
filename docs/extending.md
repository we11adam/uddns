# Adding providers and updaters

Providers obtain the current IP addresses and updaters write them to a DNS
record. Both are plugged in through a registry: an implementation registers a
constructor from its package's `init` function, and `main.go` imports the
package for that side effect.

## Configuration flow

Every key below `providers` or `updaters` is an instance. UDDNS resolves each
instance to a registered implementation, either from the key itself (for
example `updaters.cloudflare`) or from its `type` field (for example
`updaters.cf-work` with `type: cloudflare`).

Constructors always read their own config key, such as `updaters.cloudflare`.
For a named instance UDDNS calls the constructor with a config that contains
only that instance's settings, without `type`, below the implementation's key.
A constructor therefore handles legacy and named instances the same way and
must not read keys outside its own.

Updaters have one more contract: the DNS record lives in the common `domain`
field and, for DNS services with zones, the optional `zone` field. Without
`jobs`, these come from the updater settings. With `jobs`, UDDNS replaces both
with each job's `record` and `zone` before calling the constructor, so one
instance can update several records.

## Writing an updater

1. Create `updater/<name>/<name>.go` with a `Config` struct that uses
   `mapstructure` tags and includes `Domain string` with tag `domain`, plus
   `Zone string` with tag `zone` when the DNS service has zones.
2. Implement `updater.Updater`. `Update` receives only the address families the
   job requested; an empty `IPv4` or `IPv6` field means that family must not be
   changed.
3. Optionally implement `updater.RecordReader` to return the record's current
   addresses from the DNS service API. It enables `verify: updater_api` and the
   `auto` verification mode.
4. Register the constructor:

   ```go
   func init() {
   	updater.Register("Example", "updaters.example", func(v updater.ConfigReader) (updater.Updater, error) {
   		if !v.IsSet("updaters.example") {
   			return nil, updater.ErrNotConfigured
   		}

   		cfg := Config{}
   		if err := v.UnmarshalKey("updaters.example", &cfg); err != nil {
   			return nil, err
   		}
   		return New(&cfg)
   	})
   }
   ```

   The first argument is the display name used in logs; the last segment of
   the config key is the name users write in configuration. Both, and their
   spellings without `_`, `-`, or `.`, are reserved instance names.
5. Validate settings in the constructor and return descriptive errors. Use
   `internal/dnsname` to normalize records and split zones, and
   `internal/restyretry` to retry transient HTTP failures.
6. Add a blank import to `main.go`, document the settings in both READMEs, and
   add tests next to the implementation.

`updater/duckdns` is a compact example; `updater/cloudflare` also implements
`RecordReader`.

## Writing a provider

Providers follow the same steps with the `provider` package and the
`providers.<name>` key. `GetIPs` receives the requested families and should
return the addresses it found with a nil error, even when only one family is
available; return an error only when no requested address could be obtained.
UDDNS validates the result, and a provider list such as
`providers.use: [routeros, ip_service]` falls back to the next provider for any
family that is missing.

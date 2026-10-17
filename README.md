# r9s

A tiny [k9s](https://k9scli.io)-style terminal UI for Google Cloud Run.

r9s shells out to `gcloud`, so it uses your existing credentials. It has no config and no auth of its own.

- List services and jobs with readiness, scale (min/max), memory, revision, URL and age
- Describe, tail logs, edit YAML, and SSH into an instance with one key
- Switch projects and filter by name without leaving the terminal

## Requirements

- [Go](https://go.dev/dl/) 1.26+ to build
- [`gcloud`](https://cloud.google.com/sdk/docs/install), logged in (`gcloud auth login`)

## Install

```sh
go install github.com/pavel-durov/r9s/cmd/r9s@latest
```

Or from a clone: `just install` (or `just run` to build and start).

## Usage

```sh
r9s                      # opens the project picker
r9s -project my-project  # skips the picker
```

| key | action |
|---|---|
| `p` | pick project |
| `tab` | switch between services and jobs |
| `j`/`k`, arrows | move |
| `enter` / `d` | describe (in `less`) |
| `e` | edit YAML in `$EDITOR`, then `gcloud run ... replace` after a y/N confirm |
| `l` | last 1000 log lines (in `less`) |
| `L` | live tail logs, `ctrl-c` to stop |
| `s` | SSH into an instance |
| `o` | open in the GCP console |
| `/` | filter by name |
| `esc` | back |
| `r` | refresh |
| `?` | show key bindings |
| `q` | quit |

### Notes

- **SCALE** is the configured `min/max` instances (`-` if unset), not the live instance count. `gcloud run services list` doesn't report it.
- **AGE** is the time since the service's Ready condition last changed, the same source as gcloud's "LAST DEPLOYED AT". It can reset if a service goes unhealthy and recovers.
- **SSH** uses gcloud's hidden `gcloud beta run services ssh` (`alpha` for jobs, which needs `gcloud components install alpha`). The service must be deployed with `--ssh`.
- **Live tail** uses `gcloud beta run ... logs tail`, which needs `gcloud components install beta log-streaming`.

## Development

```sh
just test   # go vet + go test
just fmt
```

The code is two small packages: `pkg/cloudrun` (gcloud calls and row formatting) and `pkg/ui` ([tview](https://github.com/rivo/tview) front end).

## Contributing

Issues and PRs are welcome. Please run `just test` and `just fmt` first.

## License

[MIT](LICENSE)

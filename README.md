# EmbyCollectionSync

A small Go CLI that mirrors public MDBList or Trakt lists into Emby collections. It matches movies and series by TMDb ID, then IMDb ID, then TVDb ID. When several Emby items share an ID, it selects the lowest numeric Emby item ID (or lexical ID for nonnumeric IDs) and logs the candidates. It never guesses from titles.

## Build

Requires Go 1.24 or later.

```sh
go build -o embycollectionsync ./cmd/embycollectionsync
```

Copy `config.example.yaml` to `config.yaml` and set the environment variables referenced in it. `config.yaml` and the local ownership state file are ignored by Git. The state file is stored beside the config and should be kept with it when moving to the NAS.

```sh
export EMBY_API_KEY='...'
export MDBLIST_API_KEY='...'
export TRAKT_CLIENT_ID='...'
./embycollectionsync --config config.yaml --dry-run
./embycollectionsync --config config.yaml
```

On Synology Task Scheduler, run the binary with an absolute `--config` path and provide the variables in the task environment or a restricted wrapper script. The process exits nonzero when a collection fails; other enabled collections still run.

## Configuration

- `emby.url`: Emby server base URL, including any reverse proxy path. `emby.api_key`: Emby API key.
- `mdblist.api_key`: MDBList key from Preferences. MDBList sources use `https://mdblist.com/lists/USER/SLUG` (or `www.mdblist.com`).
- `trakt.client_id`: Trakt application client ID/API key. Trakt sources use `https://trakt.tv/users/USER/lists/SLUG`. Public lists require the client ID. Set `trakt.access_token` to an OAuth access token for private lists accessible to that account. Token renewal is outside this MVP.
- Each collection has `name`, `source` (`mdblist` or `trakt`), `url`, and `enabled`. Add `emby_id` only to explicitly adopt an existing Emby collection with that exact name. Otherwise, a same-name existing collection causes an error to protect unrelated collections.

Secrets support `${ENV_VAR}` expansion. Avoid placing keys in source URLs. The app makes no changes with `--dry-run`; it prints the proposed Emby item IDs to add/remove and a summary. Unmatched source entries are logged and skipped. An empty source response stops that collection instead of clearing it.

The app saves `.embycollectionsync-state.json` beside the config after creating or adopting a collection. It records the Emby collection ID, source, and URL. Do not delete it while managing collections; without it, a subsequent run will refuse to modify a same-name collection until an explicit `emby_id` is configured. Changing a managed collection's source requires deliberate state/config review.

## API behavior

MDBList list items are read from its list items API with `unified=true`, `limit`, and `offset`, following `X-Has-More`. Trakt list items use `page` and `limit` and follow `X-Pagination-Page-Count`. Emby movies and series are fetched in pages with `ProviderIds`, then collections are reconciled using batched item additions/removals. No external item causes an individual Emby lookup.

API references: [MDBList API and key](https://docs.mdblist.com/docs/api), [MDBList list URL format](https://docs.mdblist.com/docs/third-party/kometa), [Trakt authentication](https://docs.trakt.tv/docs/authentication-oauth), [Trakt pagination announcement](https://github.com/trakt/trakt-api/discussions/681), [Emby items query](https://dev.emby.media/reference/RestAPI/ItemsService/getItems.html), [Emby collection API](https://dev.emby.media/reference/RestAPI/CollectionService.html).

## Test

```sh
go test ./...
```

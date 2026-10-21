# kirkyards

Searchable gravestone inscriptions and photographs from kirkyards and cemeteries
around the River Tay in Fife and Dundee, Scotland. The stones were photographed
and transcribed in 2013.

This repository is the source for **kirkyards.scot**: a Go web app serving a
page per stone, with JSON and Markdown versions of every page for agents.

## Coverage

| Place | Stones | Inscriptions | Transcribed |
|---|---:|---:|---:|
| Abdie Churchyard, Fife | 100 | 103 | 62 |
| Leuchars Parish Church, Fife | 55 | 84 | 55 |
| Tayport Auld Kirk, Fife | 228 | 286 | 232 |
| Tayport Cemetery, Fife | 163 | 167 | 153 |
| Balgay Cemetery, Dundee | 341 | 370 | 103 |
| Barnhill Cemetery, Dundee | 114 | 119 | 78 |
| Eastern Cemetery, Dundee | 103 | 110 | 95 |
| Howff, Dundee | 365 | 425 | 40 |
| Western Cemetery, Dundee | 162 | 195 | 154 |

Uncertain readings in transcriptions are marked in square brackets, for
example `DIED [3?] MAY 1850`.

## Data

- `data/places.json` lists every place, with its photo directory and counts.
- `data/stones/<slug>.json` holds each place's stones. A stone has one or more
  inscriptions (one per inscribed face), each with its text and photos.

Photo paths are relative to the place's `imageDir`. The photos themselves are
kept in object storage, not in git.

## Running the site

```sh
go run ./cmd/server                 # http://localhost:8080
go test ./...
```

The data and templates are embedded, so the binary is self-contained.
It is configured by environment variable:

| Variable | Purpose |
|---|---|
| `PORT` | Listen port (default 8080) |
| `BASE_URL` | Public origin for canonical links and the API, e.g. `https://kirkyards.scot` |
| `PHOTO_BASE_URL` | Where photos are served from; without it, pages show placeholders |
| `NOINDEX` | If set, sends `X-Robots-Tag: noindex` so search engines skip a temporary address |

### URLs

| Path | What |
|---|---|
| `/` | Burial grounds |
| `/search?q=smith&place=<slug>` | Search; every word must match the start of an inscription word |
| `/<place>` | A place and its stones |
| `/<place>/<id>` | One stone |
| `/llms.txt`, `/sitemap.xml`, `/robots.txt` | For agents and search engines |
| `/api/places.json`, `/api/search.json?q=` | JSON API |

Add `.json` or `.md` to a place or stone URL for JSON or Markdown, or send an
`Accept: application/json` or `Accept: text/markdown` header.

## Photos

`cmd/photos` makes 1200px web photos and 400px thumbnails from the full-size
originals in the backup zip, laid out as `PHOTO_BASE_URL` expects:

```sh
go run ./cmd/photos -zip InscriptionImages_new.zip -out ../kirkyards-photos
```

## Regenerating the data

The data was imported from a 2013 MySQL dump of the original PHP site:

```sh
go run ./cmd/import \
  -sql saferfif_InscriptionImages.sql \
  -photos InscriptionImages_new.zip \
  -out data
```

`-photos` is optional. With it, the importer reports photo references that are
missing from the archive and monument photos not linked to any inscription.

The raw dump is not committed: it contains a visitor search log with IP
addresses, which the importer never reads.

## Licence

- Code: MIT (see `LICENSE`).
- Transcriptions: CC BY 4.0 (see `data/LICENSE.md`).

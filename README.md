# lastfmq - command-line tool/web scraper to query Last.fm artist information

Last.fm is a music recommendation service that uses data from its users to provide
music recommendations. It is a popular service for discovering new music and
keeping track of what you listen to. Last.fm has a large database of artists,
albums, and tracks, and it provides a variety of features for exploring music
and discovering new artists.

The main tool focus is on quickly exploring Last.fm artist information,
such as scrobbles, years active, and genre tags. It can also be used to query
similar artists and events. The tool is designed to be used in a pipeline,
so you can easily integrate it into your workflow. 

## Features

- Query Last.fm artist information, event years, tags, wiki

- Query similar artists (allows to specify offset in order to query unobvious
  related artists)

- No API key required, it's a web scraper

## Usage

lastfmq can be used to query Last.fm artist information by passing the artist name as an argument.
You can also pass band name as a flag for convenience.

```bash
$ lastfmq -h
lastfmq - read last.fm band information
usage: lastfmq [flags] <band_name>
  -band string
    	band name (for convenience)
  -events
    	read events
  -similar-artists
    	read similar artists
  -similar-artists-pages int
    	number of pages for similar artists (default 5)
  -similar-artists-pages-offset int
    	page offset for similar artists
  -tags
    	read artists tags
  -wiki
    	read wiki
  -wiki-ref-format string
    	the reference format for the wiki references in text (default "%q")
  -workers int
    	the number of workers (default 1)
```

The output will be in JSON format, which can be easily parsed by other tools.

## Examples

### Querying an artist

```bash
lastfmq "Fugazi" | jq
```

```json
{
  "band_name": "Fugazi",
  "scrobbles": 52384134,
  "listeners": 1357164,
  "years_active": "1987 – 2003 (16 years)",
  "founded_in": "Washington, D.C., United States"
}
```

### Querying an artist with tags

```bash
lastfmq -wiki -tags "Fugazi"
```

```json
{
  "band_name": "Fugazi",
  "scrobbles": 52384134,
  "listeners": 1357164,
  "years_active": "1987 – 2003 (16 years)",
  "founded_in": "Washington, D.C., United States",
  "tags": [
    "post-hardcore",
    "punk",
    "hardcore",
    "indie",
    "77davez-all-tracks",
    "indie rock",
    ...
  ],
  "wiki": {
    "members": [
      {
        "name": "Brendan Canty",
        "years_active": "(1987 – present)"
      },
      {
        "name": "Guy Picciotto",
        "years_active": "(1987 – present)"
      },
      {
        "name": "Ian MacKaye",
        "years_active": "(1987 – present)"
      },
      {
        "name": "Joe Lally",
        "years_active": "(1987 – present)"
      }
    ],
    "bio": [
      "Fugazi is an American post-hardcore band that formed in Washington, D.C., in 1986. The band consists of guitarists and vocalists \"Ian MacKaye\" and \"Guy Picciotto\", bassist \"Joe Lally\", and drummer \"Brendan Canty\". They are noted for their style-transcending music, DIY ethical stance, manner of business practice and contempt for the music industry.",
      ...
    ],
    "refs": [
      {
        "name": "Ian MacKaye",
        "reference": "/music/Ian+MacKaye"
      },
      ...
    ]
  },
  "similar_artists": [
    "Unwound",
    "Rites of Spring",
    "Squirrel Bait",
    "Drive Like Jehu",
    ...
  ]
}
```

## Querying an artist similar artists on page 15

```bash
lastfmq -similar-artists -similar-artists-pages 15 "Fugazi"
```

```json
{
  "band_name": "Fugazi",
  "scrobbles": 52384134,
  "listeners": 1357164,
  "years_active": "1987 – 2003 (16 years)",
  "founded_in": "Washington, D.C., United States",
  "tags": [
    "post-hardcore",
    "punk",
    "hardcore",
    "indie",
    "77davez-all-tracks",
    "indie rock",
    "alternative",
    "instrumental",
    "rock",
    "fugazi",
    "post-punk"
  ],
  "similar_artists": [
    "No Knife",
    "Scream",
    "Narrow Head",
    "The Bouncing Souls",
    "Frodus",
    ...
  ]
}
```



## Installation

### Installation via Go

To install lastfmq, you can use the following command:

```bash
    git clone https://github.com/oiweiwei/lastfmq.git
    cd lastfmq/
    go install ./latfmq.go
```

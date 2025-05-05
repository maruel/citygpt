# GeoIP Database

## About

This package provides Geographic IP lookup functionality using the MaxMind GeoLite2 or similar MMDB databases.

## Updating the GeoIP Database

To update your GeoIP database:

1. Visit https://ipinfo.io/dashboard/downloads to download the latest database file.
2. Sign up for a free account if you don't already have one.
3. Download the IP to Country database in MMDB format.

```bash
curl -Lo $HOME/.config/citygpt/ipinfo_lite.mmdb https://ipinfo.io/data/ipinfo_lite.mmdb?token=<your_token>
```

## Note

The GeoIP database is embedded into the application binary during compilation. After updating the database file, you'll need to recompile the application to use the updated data.

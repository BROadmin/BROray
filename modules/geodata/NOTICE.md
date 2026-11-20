# Sources of routing data

BROray downloads native data from these upstream projects:

- GeoIP: [v2fly/geoip](https://github.com/v2fly/geoip), selected country files from `dat/` at an immutable release commit. The upstream license is [CC BY-SA 4.0](licenses/geoip-CC-BY-SA-4.0.txt). Its [README](https://github.com/v2fly/geoip/blob/master/README.md) lists the underlying data sources and attribution.
- GeoSite: [v2fly/domain-list-community](https://github.com/v2fly/domain-list-community), `dlc.dat` at an immutable release commit, under the [MIT license](licenses/domain-list-community-MIT.txt), copyright 2018–2019 V2Ray.

The original data files are retained without conversion. Each local snapshot records the source URL, Git commit, file size and SHA-256 of the received bytes. A locally computed SHA-256 is an integrity record, not a publisher signature. BROray does not claim authorship of upstream data.

The application's release packaging must retain these notices and license texts. This module is not wired to the application updater until its mutation fence, core configuration validation and rollback integration are complete.

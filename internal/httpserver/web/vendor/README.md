# Vendored browser libraries

Committed rather than fetched at page load, so the binary stays self-contained.
Renovate cannot see these files; refresh them by hand.

| File | Package | Version | License | SHA-256 |
| --- | --- | --- | --- | --- |
| `d3.min.js` | [d3](https://www.npmjs.com/package/d3) | 7.9.0 | ISC | `f2094bbf6141b359722c4fe454eb6c4b0f0e42cc10cc7af921fc158fceb86539` |
| `adaptivecards.min.js` | [adaptivecards](https://www.npmjs.com/package/adaptivecards) | 3.0.6 | MIT | `5e7c13f3300ae7b89b34703501e08d709fbb6635f1c6755b92495577a77344f2` |

Refresh with:

```bash
curl -fsSL -o internal/httpserver/web/vendor/adaptivecards.min.js \
  https://unpkg.com/adaptivecards@<version>/dist/adaptivecards.min.js
shasum -a 256 internal/httpserver/web/vendor/adaptivecards.min.js
```

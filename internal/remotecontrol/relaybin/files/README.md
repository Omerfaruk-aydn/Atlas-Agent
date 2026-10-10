# Bundled Linux relay

`go generate ./internal/remotecontrol/relaybin` writes the compressed
`atlas-mobile-relay` binaries for linux/amd64 and linux/arm64 here, with
`manifest.json` recording each binary's size and SHA-256. The generated
files are not committed. Without them `/rc` still manages profiles, tests
connections and pairs phones, but refuses to install and says why.

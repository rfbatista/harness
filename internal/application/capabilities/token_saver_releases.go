package capabilities

import "operators-mcp/internal/domain"

// TokenSaverRelease is the release of one token saver the harness pins: what
// it downloads, from where, and how it verifies it. Updating a saver means
// updating its entry in PinnedTokenSaverReleases.
type TokenSaverRelease struct {
	Saver   domain.TokenSaver
	Summary string
	// Repo is the release's repository, owner/name: the asset's URL is
	// <release base>/<Repo>/releases/download/<Version>/<asset name>.
	Repo    string
	Version string
	// Assets are the release's asset per supported platform.
	Assets map[domain.Platform]TokenSaverAsset
	// Checksums is the release's checksum file, in sha256sum format, the
	// asset is verified against. "" when SHA256 pins the digest instead.
	Checksums string
	// SHA256 is the asset's digest, pinned here for a release that publishes
	// no checksums (caveman).
	SHA256 string
	// Only keeps just the asset's Entry, at its base name, rather than the
	// whole archive (caveman's ruleset out of its skill bundle).
	Only bool
}

// TokenSaverAsset is one platform's file of a release.
type TokenSaverAsset struct {
	// Name is the asset's file name: a .tar.gz or a zip.
	Name string
	// Entry is the path, inside the archive, of the file a session uses:
	// the executable, or caveman's ruleset.
	Entry string
}

// DefaultTokenSaverReleaseBase is where releases are downloaded from unless
// the configuration points elsewhere.
const DefaultTokenSaverReleaseBase = "https://github.com"

// PinnedTokenSaverReleases are the releases this harness installs (checked
// against each release on 2026-10-18). It returns a fresh copy each call.
func PinnedTokenSaverReleases() []TokenSaverRelease {
	codegraph := func(os, arch string) TokenSaverAsset {
		dir := "codegraph-" + os + "-" + arch
		return TokenSaverAsset{Name: dir + ".tar.gz", Entry: dir + "/bin/codegraph"}
	}
	caveman := TokenSaverAsset{Name: "caveman.skill", Entry: "caveman/SKILL.md"}
	return []TokenSaverRelease{
		{
			// rtk-oss: the Apache-2.0 build, never the BUSL rtk or rtk-proxy.
			Saver:     domain.TokenSaverRTK,
			Summary:   "Filters shell command output through a Bash hook.",
			Repo:      "rtk-ai/rtk",
			Version:   "v1.0.1",
			Checksums: "checksums.txt",
			Assets: map[domain.Platform]TokenSaverAsset{
				{OS: "darwin", Arch: "amd64"}: {Name: "rtk-oss-x86_64-apple-darwin.tar.gz", Entry: "rtk"},
				{OS: "darwin", Arch: "arm64"}: {Name: "rtk-oss-aarch64-apple-darwin.tar.gz", Entry: "rtk"},
				{OS: "linux", Arch: "amd64"}:  {Name: "rtk-oss-x86_64-unknown-linux-musl.tar.gz", Entry: "rtk"},
				{OS: "linux", Arch: "arm64"}:  {Name: "rtk-oss-aarch64-unknown-linux-gnu.tar.gz", Entry: "rtk"},
			},
		},
		{
			// Only the ruleset (ADR 0013): no binaries, proxy, hooks or CLI.
			Saver:   domain.TokenSaverCaveman,
			Summary: "A ruleset that makes the agent write tersely.",
			Repo:    "JuliusBrussee/caveman",
			Version: "v4.0.1",
			SHA256:  "765cf6fa6feaaabe0abb46936eeff8aff935e5ec49c11a2ea96a6f3775669e10",
			Only:    true,
			Assets: map[domain.Platform]TokenSaverAsset{
				{OS: "darwin", Arch: "amd64"}: caveman,
				{OS: "darwin", Arch: "arm64"}: caveman,
				{OS: "linux", Arch: "amd64"}:  caveman,
				{OS: "linux", Arch: "arm64"}:  caveman,
			},
		},
		{
			// The self-contained bundle: its own node, launched by bin/codegraph.
			Saver:     domain.TokenSaverCodegraph,
			Summary:   "A code graph the agent queries over MCP instead of crawling files.",
			Repo:      "colbymchenry/codegraph",
			Version:   "v1.6.2",
			Checksums: "SHA256SUMS",
			Assets: map[domain.Platform]TokenSaverAsset{
				{OS: "darwin", Arch: "amd64"}: codegraph("darwin", "x64"),
				{OS: "darwin", Arch: "arm64"}: codegraph("darwin", "arm64"),
				{OS: "linux", Arch: "amd64"}:  codegraph("linux", "x64"),
				{OS: "linux", Arch: "arm64"}:  codegraph("linux", "arm64"),
			},
		},
	}
}

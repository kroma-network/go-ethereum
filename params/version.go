// Copyright 2016 The go-ethereum Authors
// This file is part of the go-ethereum library.
//
// The go-ethereum library is free software: you can redistribute it and/or modify
// it under the terms of the GNU Lesser General Public License as published by
// the Free Software Foundation, either version 3 of the License, or
// (at your option) any later version.
//
// The go-ethereum library is distributed in the hope that it will be useful,
// but WITHOUT ANY WARRANTY; without even the implied warranty of
// MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE. See the
// GNU Lesser General Public License for more details.
//
// You should have received a copy of the GNU Lesser General Public License
// along with the go-ethereum library. If not, see <http://www.gnu.org/licenses/>.

package params

import (
	"fmt"
	"regexp"
	"strconv"
)

// Version is the version of upstream geth
const (
	VersionMajor = 1        // Major version component of the current release
	VersionMinor = 13       // Minor version component of the current release
	VersionPatch = 8        // Patch version component of the current release
	VersionMeta  = "stable" // Version metadata to append to the version string
)

// KromaVersion is the version of kroma-geth
var (
	KromaVersionMajor = 0          // Major version component of the current release
	KromaVersionMinor = 1          // Minor version component of the current release
	KromaVersionPatch = 0          // Patch version component of the current release
	KromaVersionMeta  = "unstable" // Version metadata to append to the version string
)

// This is set at build-time by the linker when the build is done by build/ci.go.
var gitTag string

// Override the version variables if the gitTag was set at build time.
var _ = func() (_ string) {
	semver := regexp.MustCompile(`^v([0-9]+)\.([0-9]+)\.([0-9]+)(?:-([0-9A-Za-z-]+(?:\.[0-9A-Za-z-]+)*))?(?:\+[0-9A-Za-z-]+)?$`)
	version := semver.FindStringSubmatch(gitTag)
	if version == nil {
		return
	}
	if version[4] == "" {
		version[4] = "stable"
	}
	KromaVersionMajor, _ = strconv.Atoi(version[1])
	KromaVersionMinor, _ = strconv.Atoi(version[2])
	KromaVersionPatch, _ = strconv.Atoi(version[3])
	KromaVersionMeta = version[4]
	return
}()

// Version holds the textual version string.
var Version = func() string {
	return fmt.Sprintf("%d.%d.%d", KromaVersionMajor, KromaVersionMinor, KromaVersionPatch)
}()

// VersionWithMeta holds the textual version string including the metadata.
var VersionWithMeta = func() string {
	v := Version
	if KromaVersionMeta != "" {
		v += "-" + KromaVersionMeta
	}
	return v
}()

// GethVersion holds the textual geth version string.
var GethVersion = func() string {
	return fmt.Sprintf("%d.%d.%d", VersionMajor, VersionMinor, VersionPatch)
}()

// GethVersionWithMeta holds the textual geth version string including the metadata.
var GethVersionWithMeta = func() string {
	v := GethVersion
	if VersionMeta != "" {
		v += "-" + VersionMeta
	}
	return v
}()

// ArchiveVersion holds the textual version string used for Geth archives. e.g.
// "1.8.11-dea1ce05" for stable releases, or "1.8.13-unstable-21c059b6" for unstable
// releases.
func ArchiveVersion(gitCommit string) string {
	vsn := Version
	if KromaVersionMeta != "stable" {
		vsn += "-" + KromaVersionMeta
	}
	if len(gitCommit) >= 8 {
		vsn += "-" + gitCommit[:8]
	}
	return vsn
}

func VersionWithCommit(gitCommit, gitDate string) string {
	vsn := VersionWithMeta
	if len(gitCommit) >= 8 {
		vsn += "-" + gitCommit[:8]
	}
	if (KromaVersionMeta != "stable") && (gitDate != "") {
		vsn += "-" + gitDate
	}
	return vsn
}

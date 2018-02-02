// Copyright 2014-2016 Amazon.com, Inc. or its affiliates. All Rights Reserved.
//
// Licensed under the Apache License, Version 2.0 (the "License"). You may
// not use this file except in compliance with the License. A copy of the
// License is located at
//
//	http://aws.amazon.com/apache2.0/
//
// or in the "license" file accompanying this file. This file is distributed
// on an "AS IS" BASIS, WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either
// express or implied. See the License for the specific language governing
// permissions and limitations under the License.

package utils

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"
)

type Version string

type semver struct {
	major             int
	minor             int
	patch             int
	preReleaseVersion string
	buildMetadata     string
}

// Matches returns whether or not a version matches a given selector.
// The selector can be any of the following:
//
// * x.y.z -- Matches a version exactly the same as the selector version
// * >=x.y.z -- Matches a version greater than or equal to the selector version
// * >x.y.z -- Matches a version greater than the selector version
// * <=x.y.z -- Matches a version less than or equal to the selector version
// * <x.y.z -- Matches a version less than the selector version
// * x.y.z,a.b.c -- Matches if the version matches either of the two selector versions
func (lhs Version) Matches(selector string) (bool, error) {
	lhsVersion, err := parseSemver(string(lhs))
	if err != nil {
		return false, err
	}

	if strings.Contains(selector, ",") {
		orElements := strings.Split(selector, ",")
		for _, el := range orElements {
			if elMatches, err := lhs.Matches(el); err != nil {
				return false, err
			} else if elMatches {
				return true, nil
			}
		}
		// No elements matched
		return false, nil
	}

	if strings.HasPrefix(selector, ">=") {
		rhsVersion, err := parseSemver(selector[2:])
		if err != nil {
			return false, err
		}
		return compareSemver(lhsVersion, rhsVersion) >= 0, nil
	} else if strings.HasPrefix(selector, ">") {
		rhsVersion, err := parseSemver(selector[1:])
		if err != nil {
			return false, err
		}
		return compareSemver(lhsVersion, rhsVersion) > 0, nil
	} else if strings.HasPrefix(selector, "<=") {
		rhsVersion, err := parseSemver(selector[2:])
		if err != nil {
			return false, err
		}
		return compareSemver(lhsVersion, rhsVersion) <= 0, nil
	} else if strings.HasPrefix(selector, "<") {
		rhsVersion, err := parseSemver(selector[1:])
		if err != nil {
			return false, err
		}
		return compareSemver(lhsVersion, rhsVersion) < 0, nil
	}

	rhsVersion, err := parseSemver(selector)
	if err != nil {
		return false, err
	}
	return compareSemver(lhsVersion, rhsVersion) == 0, nil
}

func parseSemver(version string) (semver, error) {
	var result semver
	// 0.0.0-some-prealpha-stuff.1+12345
	versionAndMetadata := strings.SplitN(version, "+", 2)
	// [0.0.0-some-prealpha-stuff.1, 12345]
	if len(versionAndMetadata) == 2 {
		result.buildMetadata = versionAndMetadata[1]
	}
	versionAndPrerelease := strings.SplitN(versionAndMetadata[0], "-", 2)
	// [0.0.0, some-prealpha-stuff.1]
	if len(versionAndPrerelease) == 2 {
		result.preReleaseVersion = versionAndPrerelease[1]
	}
	versionStr := versionAndPrerelease[0]
	versionParts := strings.SplitN(versionStr, ".", 3)
	// [0, 0, 0]
	if len(versionParts) != 3 {
		return result, fmt.Errorf("Not enough '.' characters in the version part")
	}
	major, err := strconv.Atoi(versionParts[0])
	if err != nil {
		return result, fmt.Errorf("Cannot parse major version as int: %v", err)
	}
	minor, err := strconv.Atoi(versionParts[1])
	if err != nil {
		return result, fmt.Errorf("Cannot parse minor version as int: %v", err)
	}
	patch, err := strconv.Atoi(versionParts[2])
	if err != nil {
		return result, fmt.Errorf("Cannot parse patch version as int: %v", err)
	}
	result.major = major
	result.minor = minor
	result.patch = patch
	return result, nil
}

// compareSemver compares two semvers, 'lhs' and 'rhs', and returns -1 if lhs is less
// than rhs, 0 if they are equal, and 1 lhs is greater than rhs
func compareSemver(lhs, rhs semver) int {
	if lhs.major < rhs.major {
		return -1
	}
	if lhs.major > rhs.major {
		return 1
	}
	if lhs.minor < rhs.minor {
		return -1
	}
	if lhs.minor > rhs.minor {
		return 1
	}
	if lhs.patch < rhs.patch {
		return -1
	}
	if lhs.patch > rhs.patch {
		return 1
	}
	if lhs.preReleaseVersion != "" && rhs.preReleaseVersion == "" {
		return -1
	}
	if lhs.preReleaseVersion == "" && rhs.preReleaseVersion != "" {
		return 1
	}
	if lhs.preReleaseVersion < rhs.preReleaseVersion {
		return -1
	}
	if lhs.preReleaseVersion > rhs.preReleaseVersion {
		return 1
	}
	return 0
}

// ExtractVersion extracts a matching version from the version number string
func ExtractVersion(input string) string {
	versionNumberRegex := regexp.MustCompile(` v(\d+\.\d+\.\d+(\-[\S\.\-]+)?(\+[\S\.\-]+)?)`)
	versionNumberStr := versionNumberRegex.FindStringSubmatch(input)
	if len(versionNumberStr) >= 2 {
		return string(versionNumberStr[1])
	}
	return "UNKNOWN"
}

package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/ioutil"
	"net/http"
	"runtime"

	"github.com/blang/semver"
	"github.com/inconshreveable/go-update"
)

const (
	binaryReleaseTpl      = "safe-%s-amd64"
	safeGithubReleasesURL = "https://api.github.com/repos/starkandwayne/safe/releases"
)

type githubRelease struct {
	Assets  []*githubReleaseAsset `json:"assets"`
	TagName string                `json:"tag_name"`
}

type githubReleaseAsset struct {
	BrowserDownloadURL string `json:"browser_download_url"`
	Name               string `json:"name"`
}

func findLatestRelease(releases []*githubRelease) (*githubRelease, error) {
	var latest *githubRelease

	for _, r := range releases {
		if latest == nil {
			// Guard against setting a release that does not follow semver
			_, err := semver.Make(r.TagName)
			if err != nil {
				latest = r
			}
		} else {
			latestVer, _ := semver.Make(latest.TagName)
			currentVer, err := semver.Make(r.TagName)

			if err == nil && currentVer.GT(latestVer) {
				latest = r
			}
		}
	}

	if latest == nil {
		return nil, errors.New("Unable to find latest release")
	}
	return latest, nil
}

func findAssetForOS(r *githubRelease) (*githubReleaseAsset, error) {
	name := fmt.Sprintf(binaryReleaseTpl, runtime.GOOS)

	for _, ra := range r.Assets {
		if ra.Name == name {
			return ra, nil
		}
	}

	return nil, fmt.Errorf("Release '%s' does not contain asset '%s'", r.TagName, name)
}

func readGithubReleases(url string) ([]*githubRelease, error) {
	resp, err := http.Get(safeGithubReleasesURL)
	if err != nil {
		return nil, fmt.Errorf("Unable to retrieve releases from GitHub: '%s'", err)
	}
	defer resp.Body.Close()
	body, err := ioutil.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("Could not read response from GitHub: '%s'", err)
	}

	releases := []*githubRelease{}
	err = json.Unmarshal(body, &releases)
	if err != nil {
		return nil, fmt.Errorf("Could not unmarshal JSON: '%s'", err)
	}

	return releases, nil
}

func updateBinary(url string) error {
	resp, err := http.Get(url)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	return update.Apply(resp.Body, update.Options{})
}

package httpapi

import (
	"encoding/json"
	"net/http"
	"strings"
	"time"

	"gptmail/internal/version"

	"github.com/gin-gonic/gin"
)

func (h *Handler) health(c *gin.Context) {
	ok(c, gin.H{"status": "ok", "time": time.Now().Format(time.RFC3339)})
}

func (h *Handler) versionInfo(c *gin.Context) {
	ok(c, gin.H{
		"version":   version.Version,
		"commit":    version.Commit,
		"buildTime": version.BuildTime,
	})
}

func (h *Handler) versionCheck(c *gin.Context) {
	if !h.requireAdmin(c) {
		return
	}

	currentVersion := version.Version

	latestVersion := currentVersion
	updateAvailable := false
	releaseURL := ""

	req, err := http.NewRequestWithContext(c.Request.Context(), "GET", "https://api.github.com/repos/hloolx/HloolMail/releases/latest", nil)
	if err == nil {
		req.Header.Set("Accept", "application/vnd.github+json")
		req.Header.Set("User-Agent", "hloolmail-version-check")
		resp, fetchErr := http.DefaultClient.Do(req)
		if fetchErr == nil {
			defer resp.Body.Close()
			if resp.StatusCode == 200 {
				var release struct {
					TagName string `json:"tag_name"`
					HTMLURL string `json:"html_url"`
				}
				if json.NewDecoder(resp.Body).Decode(&release) == nil && release.TagName != "" {
					tagVersion := strings.TrimPrefix(release.TagName, "v")
					if tagVersion != "" && tagVersion != currentVersion && currentVersion != "dev" {
						latestVersion = tagVersion
						updateAvailable = true
						releaseURL = release.HTMLURL
					} else if tagVersion != "" {
						latestVersion = tagVersion
					}
				}
			}
		}
	}

	ok(c, gin.H{
		"currentVersion":  currentVersion,
		"latestVersion":   latestVersion,
		"updateAvailable": updateAvailable,
		"releaseURL":      releaseURL,
	})
}

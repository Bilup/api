package main

import (
	"os"
	"strings"
	"time"
)

// ---- storage paths (main.osl) ----
var (
	projectsDir        = "data/projects/"
	indexFile          = "data/projects-index.json"
	assetsIndexFile    = "data/assets-index.json"
	usersDir           = "data/users/"
	sessionsDir        = "data/sessions/"
	commentsDir        = "data/comments/"
	reportsDir         = "data/reports/"
	reportsFile        = reportsDir + "reports.json"
	reportEvidenceDir  = "data/report-evidence/"
	newsFile           = "data/news.json"
	notificationsDir   = "data/notifications/"
	activityDir        = "data/activity/"
	userSettingsDir    = "data/usersettings/"
	tmpDir             = "data/tmp/"
	stagingDir         = "data/staging/"
	thumbnailsDir      = "data/thumbnails/"
	quotaDir           = "data/quota/"
	userIdsFile        = "data/userids.json"
	adminsFile         = "data/admins.json"
	bansFile           = "data/bans.json"
	agreementAcceptDir = "data/agreement-acceptances/"
	extensionsDir      = "data/extensions/"
	extensionsFile     = "data/extensions.json"
	purchaseKeysFile   = "data/purchase-keys.json"
	pendingPayoutsFile = "data/pending-payouts.json"
)

// ---- limits (main.osl) ----
const (
	maxProjectJsonBytes       float64 = 1073741824
	maxStoredProjectJsonBytes float64 = 20971520
	maxAssetBytes             float64 = 10485760
	maxThumbBytes             float64 = 1048576
	maxAssetCount                     = 2000
	maxProjectAssetsBytes     float64 = 52428800
	maxExtractedProjectBytes  float64 = maxProjectJsonBytes + maxProjectAssetsBytes
	weeklyUploadQuotaBytes    float64 = 104857600
	quotaWindowMs                     = 604800000
	uploadDebounceMs                   = 86400000
	maxProjectExtensions               = 100
	maxExtensionSourceBytes   float64 = 2097152
	maxProjectExtensionBytes  float64 = 10485760
)

// ---- config values (main.osl) ----
var (
	appURL              string
	port                string
	roturAppKey         string
	r2Endpoint          string
	r2Bucket            string
	r2AccessKeyId       string
	r2SecretAccessKey   string
	r2PublicBase        string
	giteaURL            string
	giteaAdminToken     string
	editorOrigin        string
	corsAllowedOrigins  []string
	adminUsers          []string
	bilupRoturToken  string
	bilupRoturUser   string
	roturBase           string
	paywallFeePercent   float64
	r2Local             bool
	localBlobsDir       = "data/blobs/"
)

// ---- caches (main.osl) ----
var (
	viewDedup     *cache
	viewIndexFill *cache
	userIdsCache  *cache
	sessionCache  *cache
	bansCache     *cache
	adminsCache   *cache
)

var dotenvVals = map[string]string{}

func loadDotenv() {
	data := fsReadFile(".env")
	if data == "" {
		return
	}
	for _, line := range strings.Split(data, "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") || !strings.Contains(line, "=") {
			continue
		}
		parts := strings.SplitN(line, "=", 2)
		key := strings.TrimSpace(parts[0])
		val := strings.TrimSpace(parts[1])
		dotenvVals[key] = strings.Trim(val, "\"'")
	}
}

func configVal(name, def string) string {
	if v := os.Getenv(name); v != "" {
		return v
	}
	if v := dotenvVals[name]; v != "" {
		return v
	}
	return def
}

func loadConfig() {
	appURL = strings.TrimRight(configVal("APP_URL", "https://api.bilup.org"), "/")
	port = configVal("PORT", "5627")
	roturAppKey = configVal("ROTUR_APP_KEY", "bilup")
	r2Endpoint = configVal("R2_ENDPOINT", "")
	r2Bucket = configVal("R2_BUCKET", "bilup")
	r2AccessKeyId = configVal("R2_ACCESS_KEY_ID", "")
	r2SecretAccessKey = configVal("R2_SECRET_ACCESS_KEY", "")
	r2PublicBase = configVal("R2_PUBLIC_BASE", "")
	giteaURL = configVal("GITEA_URL", "https://git.bilup.org")
	giteaAdminToken = configVal("GITEA_ADMIN_TOKEN", "")
	editorOrigin = configVal("EDITOR_ORIGIN", "https://app.bilup.org")
	corsAllowedOrigins = splitCsv(configVal("CORS_ORIGINS", editorOrigin+",http://localhost:8601,http://localhost:8602"))
	adminUsers = splitCsv(strings.ToLower(configVal("ADMIN_USERS", "RyaninCn11")))
	bilupRoturToken = configVal("BILUP_ROTUR_TOKEN", "")
	bilupRoturUser = configVal("BILUP_ROTUR_USER", "")
	roturBase = configVal("ROTUR_BASE", "https://api.accounts.bilup.org")
	paywallFeePercent = toFloat(configVal("PAYWALL_FEE_PERCENT", "10"))

	r2Local = r2Endpoint == "" || r2AccessKeyId == ""
	if r2Local {
		fsMkdirAll(localBlobsDir)
	}
}

func splitCsv(s string) []string {
	parts := strings.Split(s, ",")
	out := []string{}
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if p != "" {
			out = append(out, p)
		}
	}
	return out
}

func nowMs() int64 { return time.Now().UnixMilli() }
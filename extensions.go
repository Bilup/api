package main

import "strings"

// extensions.go — maps extensions.osl (extension policy / source / usage engine).

func loadExtensionPolicy() Obj {
	res := readFile(extensionsFile)
	if !res.isOk() {
		return Obj{"hashes": Obj{}, "blockedUrls": []any{}}
	}
	policy := toObj(res.unwrap())
	if policy["hashes"] == nil {
		policy["hashes"] = Obj{}
	}
	if policy["blockedUrls"] == nil {
		policy["blockedUrls"] = []any{}
	}
	return policy
}

func saveExtensionPolicy(policy Obj) bool { return writeFile(extensionsFile, policy) }

func isExtensionHash(hash string) bool { return regexMatch("^[a-f0-9]{64}$", hash) }

func extensionSourcePath(hash string) string { return extensionsDir + hash + ".js" }
func extensionRecordPath(hash string) string { return extensionsDir + hash + ".json" }

func extensionReviewUrl(extensionUrl string) string {
	if strings.HasPrefix(extensionUrl, "data:") {
		return "data:"
	}
	return extensionUrl
}

func isGalleryExtensionUrl(extensionUrl string) bool {
	return strings.HasPrefix(extensionUrl, "https://extensions.turbowarp.org/") ||
		strings.HasPrefix(extensionUrl, "https://extensions.mistium.com/")
}

func extensionReviewUrls(urls []any) []any {
	reviewUrls := []any{}
	for _, u := range urls {
		reviewUrl := extensionReviewUrl(toString(u))
		if !containsStr(reviewUrls, toString(reviewUrl)) {
			reviewUrls = append(reviewUrls, reviewUrl)
		}
	}
	return reviewUrls
}

func extensionHeaderValue(line, prefix string) string {
	value := strings.TrimSpace(line[len(prefix):])
	if len(value) > 500 {
		return value[:500]
	}
	return value
}

func extensionMetadata(hash string) Obj {
	metadata := Obj{}
	if !fsExists(extensionSourcePath(hash)) {
		return metadata
	}
	source := fsReadFile(extensionSourcePath(hash))
	lines := strings.Split(source, "\n")
	for i := 0; i < len(lines) && i < 50; i++ {
		line := strings.TrimSpace(lines[i])
		if strings.HasPrefix(line, "// Name:") {
			metadata["name"] = extensionHeaderValue(line, "// Name:")
		}
		if strings.HasPrefix(line, "// ID:") {
			metadata["id"] = extensionHeaderValue(line, "// ID:")
		}
		if strings.HasPrefix(line, "// Description:") {
			metadata["description"] = extensionHeaderValue(line, "// Description:")
		}
		if strings.HasPrefix(line, "// By:") {
			metadata["author"] = extensionHeaderValue(line, "// By:")
		}
		if strings.HasPrefix(line, "// License:") {
			metadata["license"] = extensionHeaderValue(line, "// License:")
		}
	}
	return metadata
}

func readExtensionRecord(hash string) Obj {
	res := readFile(extensionRecordPath(hash))
	if res.isOk() {
		return toObj(res.unwrap())
	}
	return Obj{}
}

func extensionRecord(policy Obj, hash string) Obj {
	record := readExtensionRecord(hash)
	if len(objKeys(record)) > 0 {
		return record
	}
	if hashes, ok := policy["hashes"].(map[string]any); ok {
		if hashes[hash] != nil {
			return toObj(hashes[hash])
		}
	}
	return Obj{}
}

func extensionRecordIsGallery(record Obj) bool {
	if record["gallery"] == true {
		return true
	}
	if record["urls"] != nil {
		for _, u := range asArray(record["urls"]) {
			if isGalleryExtensionUrl(toString(u)) {
				return true
			}
		}
	}
	return false
}

func extensionStatus(policy Obj, hash, sourceUrl string) string {
	for _, u := range asArray(policy["blockedUrls"]) {
		if toString(u) == sourceUrl {
			return "blocked"
		}
	}
	record := extensionRecord(policy, hash)
	if extensionRecordIsGallery(record) {
		return "trusted"
	}
	if record["status"] != nil {
		return toString(record["status"])
	}
	return "untrusted"
}

func inspectProjectExtensionUrls(jsonPath string) Obj {
	stream := openJSONStream(jsonPath, maxProjectJsonBytes)
	if !stream.ok() {
		return Obj{"ok": false, "error": "invalid extension URLs"}
	}
	unique := Obj{}
	found := float64(0)
	for stream.more() {
		event := stream.next()
		if toString(event["type"]) == "key" && toString(event["value"]) == "extensionURLs" {
			res := stream.readStringMap(float64(maxProjectExtensions) - found)
			if res.isErr() {
				stream.close()
				return Obj{"ok": false, "error": "invalid extension URLs"}
			}
			entries := toObj(res.unwrap())
			found += float64(len(objKeys(entries)))
			for _, id := range objKeys(entries) {
				unique[toString(entries[id])] = true
			}
		}
	}
	valid := stream.ok()
	stream.close()
	if !valid {
		return Obj{"ok": false, "error": "invalid extension URLs"}
	}
	urls := objKeys(unique)
	if float64(len(urls)) > maxProjectExtensions {
		return Obj{"ok": false, "error": "too many custom extensions"}
	}
	return Obj{"ok": true, "urls": anyList(urls)}
}

func parseExtensionSources(rawSources string) Obj {
	if rawSources == "" {
		return Obj{"ok": true, "sources": Obj{}}
	}
	parsed := tryParseJSON(rawSources, "object")
	if parsed.isErr() {
		return Obj{"ok": false, "error": "invalid extension sources"}
	}
	return Obj{"ok": true, "sources": parsed.unwrap()}
}

func collectProjectExtensions(jsonPath, rawSources string) Obj {
	inspected := inspectProjectExtensionUrls(jsonPath)
	if !toBool(inspected["ok"]) {
		return inspected
	}
	parsed := parseExtensionSources(rawSources)
	if !toBool(parsed["ok"]) {
		return parsed
	}
	sources := toObj(parsed["sources"])
	policy := loadExtensionPolicy()
	extensions := []any{}
	totalBytes := float64(0)

	for _, u := range asArray(inspected["urls"]) {
		extensionUrl := toString(u)
		if !strings.HasPrefix(extensionUrl, "https://") && !strings.HasPrefix(extensionUrl, "http://") && !strings.HasPrefix(extensionUrl, "data:") {
			return Obj{"ok": false, "error": "custom extension URL cannot be saved: " + extensionUrl}
		}
		if isGalleryExtensionUrl(extensionUrl) {
			extensions = append(extensions, Obj{"url": extensionUrl, "hash": sha256Hex(extensionUrl), "gallery": true})
			continue
		}
		if sources[extensionUrl] == nil {
			return Obj{"ok": false, "error": "could not read custom extension source: " + extensionUrl}
		}
		rawSource := sources[extensionUrl]
		if typeOf(rawSource) != "string" {
			return Obj{"ok": false, "error": "invalid custom extension source: " + extensionUrl}
		}
		source := toString(rawSource)
		if source == "" || float64(len(source)) > maxExtensionSourceBytes {
			return Obj{"ok": false, "error": "custom extension source is empty or exceeds 2 MB: " + extensionUrl}
		}
		totalBytes += float64(len(source))
		if totalBytes > maxProjectExtensionBytes {
			return Obj{"ok": false, "error": "custom extensions exceed the 10 MB total limit"}
		}
		hash := sha256Hex(source)
		if extensionStatus(policy, hash, extensionUrl) == "blocked" {
			return Obj{"ok": false, "error": "this project uses a blocked extension: " + extensionUrl, "code": "blocked_extension"}
		}
		extensions = append(extensions, Obj{"url": extensionUrl, "hash": hash})
	}
	return Obj{"ok": true, "extensions": extensions, "sources": sources}
}

func removeProjectExtensionUsage(projectId string, extensions []any) bool {
	policy := loadExtensionPolicy()
	seen := Obj{}
	for _, it := range extensions {
		extension := toObj(it)
		hash := toString(extension["hash"])
		if !isExtensionHash(hash) {
			return false
		}
		if seen[hash] == nil {
			lockName := "extension-" + hash
			lockKey(lockName)
			record := extensionRecord(policy, hash)
			if record["projects"] != nil {
				record["projects"] = removeStr(asArray(record["projects"]), projectId)
				if !writeFile(extensionRecordPath(hash), record) {
					unlockKey(lockName)
					return false
				}
			}
			unlockKey(lockName)
			seen[hash] = true
		}
	}
	return true
}

func storeProjectExtensions(projectId string, previousExtensions, extensions []any, sources Obj) bool {
	if !removeProjectExtensionUsage(projectId, previousExtensions) {
		return false
	}
	policy := loadExtensionPolicy()
	for _, it := range extensions {
		extension := toObj(it)
		hash := toString(extension["hash"])
		if !isExtensionHash(hash) {
			return false
		}
		extensionUrl := toString(extension["url"])
		reviewUrl := extensionReviewUrl(extensionUrl)
		gallery := isGalleryExtensionUrl(extensionUrl)
		lockName := "extension-" + hash
		lockKey(lockName)
		if !gallery && !fsExists(extensionSourcePath(hash)) && sources[extensionUrl] != nil {
			if !fsWriteFile(extensionSourcePath(hash), toString(sources[extensionUrl])) {
				unlockKey(lockName)
				return false
			}
		}
		record := extensionRecord(policy, hash)
		if len(objKeys(record)) == 0 {
			record = Obj{"status": "untrusted", "urls": []any{}, "projects": []any{}, "firstSeen": timestamp()}
		}
		if record["urls"] == nil {
			record["urls"] = []any{}
		}
		if record["projects"] == nil {
			record["projects"] = []any{}
		}
		if gallery {
			record["status"] = "trusted"
			record["gallery"] = true
		}
		recordUrls := asArray(record["urls"])
		projects := asArray(record["projects"])
		if !containsStr(recordUrls, reviewUrl) {
			recordUrls = append(recordUrls, reviewUrl)
		}
		if !containsStr(projects, projectId) {
			projects = append(projects, projectId)
		}
		record["urls"] = recordUrls
		record["projects"] = projects
		if !writeFile(extensionRecordPath(hash), record) {
			unlockKey(lockName)
			return false
		}
		unlockKey(lockName)
	}
	return true
}

func storedProjectExtensions(project Obj) []any { return asArray(project["extensions"]) }

func extensionUsageMigrated(state *result) bool {
	if !state.isOk() {
		return false
	}
	data := toObj(state.unwrap())
	if data["version"] == nil {
		return false
	}
	return toFloat(data["version"]) >= 1
}

func migrateExtensionUsage() bool {
	statePath := extensionsDir + "index.json"
	state := readFile(statePath)
	if extensionUsageMigrated(state) {
		return true
	}
	lockKey("extension-index-migration")
	state = readFile(statePath)
	if extensionUsageMigrated(state) {
		unlockKey("extension-index-migration")
		return true
	}
	for _, name := range fsReadDir(projectsDir) {
		res := readFile(projectsDir + name)
		if res.isOk() {
			project := toObj(res.unwrap())
			extensions := storedProjectExtensions(project)
			if len(extensions) > 0 && !storeProjectExtensions(toString(project["id"]), []any{}, extensions, Obj{}) {
				unlockKey("extension-index-migration")
				return false
			}
		}
	}
	ok := writeFile(statePath, Obj{"version": float64(1)})
	unlockKey("extension-index-migration")
	return ok
}

func projectTrustedExtensions(project Obj) []any {
	result := []any{}
	if project["extensions"] == nil {
		return result
	}
	policy := loadExtensionPolicy()
	for _, it := range asArray(project["extensions"]) {
		extension := toObj(it)
		hash := toString(extension["hash"])
		extensionUrl := toString(extension["url"])
		if !isGalleryExtensionUrl(extensionUrl) && extensionStatus(policy, hash, extensionUrl) == "trusted" {
			result = append(result, sha256Hex(extensionUrl))
		}
	}
	return result
}

func projectContainsExtensionHash(project Obj, hash string) bool {
	if project["extensions"] == nil {
		return false
	}
	for _, it := range asArray(project["extensions"]) {
		if toString(toObj(it)["hash"]) == hash {
			return true
		}
	}
	return false
}

func inspectStoredProjectExtensionUrls(project Obj) Obj {
	workId := toString(project["id"]) + "." + randomString(8)
	gzipPath := tmpDir + workId + ".json.gz"
	jsonPath := tmpDir + workId + ".json"
	if !copyStoredProjectJsonToPath(project, gzipPath) || !gunzip(gzipPath, jsonPath) {
		fsRemove(gzipPath)
		fsRemove(jsonPath)
		return Obj{"ok": false, "urls": []any{}}
	}
	inspected := inspectProjectExtensionUrls(jsonPath)
	fsRemove(gzipPath)
	fsRemove(jsonPath)
	return inspected
}

func projectContainsExtensionUrl(project Obj, extensionUrl string) bool {
	if project["extensions"] != nil {
		for _, it := range asArray(project["extensions"]) {
			if toString(toObj(it)["url"]) == extensionUrl {
				return true
			}
		}
		return false
	}
	inspected := inspectStoredProjectExtensionUrls(project)
	if !toBool(inspected["ok"]) {
		return false
	}
	return containsStr(asArray(inspected["urls"]), extensionUrl)
}

func projectBlockedExtension(project Obj) string {
	if project["extensions"] == nil {
		return ""
	}
	policy := loadExtensionPolicy()
	for _, it := range asArray(project["extensions"]) {
		extension := toObj(it)
		extensionUrl := toString(extension["url"])
		if extensionStatus(policy, toString(extension["hash"]), extensionUrl) == "blocked" {
			return extensionReviewUrl(extensionUrl)
		}
	}
	return ""
}

func unshareProjectForBlockedExtension(project Obj) bool {
	project["shared"] = false
	project["visibility"] = "private"
	project["edited"] = timestamp()
	project["viewKey"] = randomString(16)
	if !saveProject(project) || !upsertIndexEntry(project) {
		return false
	}
	profile := loadProfile(toString(project["owner"]))
	if toString(profile["featuredProject"]) == toString(project["id"]) {
		profile["featuredProject"] = ""
		saveProfile(profile)
	}
	addNotification(toString(project["owner"]), "MistWarp", "moderation", Obj{"message": "Your project \"" + toString(project["title"]) + "\" uses an extension that has been blocked. Remove the extension before sharing the project again."})
	return true
}

func unshareProjectsUsingHash(hash string) Obj {
	affected := float64(0)
	for _, name := range fsReadDir(projectsDir) {
		res := readFile(projectsDir + name)
		if res.isOk() {
			project := toObj(res.unwrap())
			if projectContainsExtensionHash(project, hash) {
				if !unshareProjectForBlockedExtension(project) {
					return Obj{"ok": false, "affected": affected}
				}
				affected++
			}
		}
	}
	return Obj{"ok": true, "affected": affected}
}

func unshareProjectsUsingUrl(extensionUrl string) Obj {
	affected := float64(0)
	for _, name := range fsReadDir(projectsDir) {
		res := readFile(projectsDir + name)
		if res.isOk() {
			project := toObj(res.unwrap())
			if projectContainsExtensionUrl(project, extensionUrl) {
				if !unshareProjectForBlockedExtension(project) {
					return Obj{"ok": false, "affected": affected}
				}
				affected++
			}
		}
	}
	return Obj{"ok": true, "affected": affected}
}

func handleGetProjectExtensionSource(c *Context) {
	res := loadProject(c.param("id"))
	if !res.isOk() {
		c.notFound("project not found")
		return
	}
	project := toObj(res.unwrap())
	urlHash := c.param("hash")
	if !canViewProjectJson(project, c) || !isExtensionHash(urlHash) {
		c.notFound("extension not found")
		return
	}
	sourceHash := ""
	if project["extensions"] != nil {
		policy := loadExtensionPolicy()
		for _, it := range asArray(project["extensions"]) {
			extension := toObj(it)
			extensionUrl := toString(extension["url"])
			if sha256Hex(extensionUrl) == urlHash && extensionStatus(policy, toString(extension["hash"]), extensionUrl) != "blocked" {
				sourceHash = toString(extension["hash"])
			}
		}
	}
	path := extensionSourcePath(sourceHash)
	if sourceHash == "" || !fsExists(path) {
		c.notFound("extension not found")
		return
	}
	c.setHeader("Content-Type", "application/javascript; charset=utf-8")
	c.setHeader("Cache-Control", "private, max-age=31536000, immutable")
	c.file(path)
}

func ensureExtensionRecords(policy Obj) bool {
	for _, hash := range objKeys(toObj(policy["hashes"])) {
		if isExtensionHash(hash) && !fsExists(extensionRecordPath(hash)) {
			record := toObj(toObj(policy["hashes"])[hash])
			if record["urls"] == nil {
				record["urls"] = []any{}
			}
			if record["projects"] == nil {
				record["projects"] = []any{}
			}
			lockName := "extension-" + hash
			lockKey(lockName)
			saved := fsExists(extensionRecordPath(hash)) || writeFile(extensionRecordPath(hash), record)
			unlockKey(lockName)
			if !saved {
				return false
			}
		}
	}
	for _, fileName := range fsReadDir(extensionsDir) {
		if strings.HasSuffix(fileName, ".js") {
			parts := strings.Split(fileName, ".")
			hash := ""
			if len(parts) >= 2 {
				hash = parts[1]
			}
			if isExtensionHash(hash) && !fsExists(extensionRecordPath(hash)) {
				lockName := "extension-" + hash
				lockKey(lockName)
				saved := fsExists(extensionRecordPath(hash)) || writeFile(extensionRecordPath(hash), Obj{"status": "untrusted", "urls": []any{}, "projects": []any{}, "firstSeen": timestamp()})
				unlockKey(lockName)
				if !saved {
					return false
				}
			}
		}
	}
	return true
}

func handleAdminGetExtensions(c *Context) {
	policy := loadExtensionPolicy()
	if !ensureExtensionRecords(policy) {
		c.internalError("could not prepare extension index")
		return
	}
	extensions := []any{}
	for _, fileName := range fsReadDir(extensionsDir) {
		if strings.HasSuffix(fileName, ".json") {
			hash := strings.Split(fileName, ".")[1]
			if isExtensionHash(hash) {
				record := readExtensionRecord(hash)
				urls := []any{}
				projects := []any{}
				if record["urls"] != nil {
					urls = extensionReviewUrls(asArray(record["urls"]))
				}
				if record["projects"] != nil {
					projects = asArray(record["projects"])
				}
				status := "untrusted"
				if record["status"] != nil {
					status = toString(record["status"])
				}
				firstSeen := float64(0)
				if record["firstSeen"] != nil {
					firstSeen = toFloat(record["firstSeen"])
				}
				gallery := extensionRecordIsGallery(record)
				if gallery {
					status = "trusted"
				}
				extensions = append(extensions, Obj{
					"hash":            hash,
					"status":          status,
					"urls":            urls,
					"projects":        projects,
					"projectCount":    float64(len(projects)),
					"firstSeen":       firstSeen,
					"sourceAvailable": fsExists(extensionSourcePath(hash)),
					"metadata":        extensionMetadata(hash),
					"gallery":         gallery,
				})
			}
		}
	}
	blockedUrls := []any{}
	for _, u := range asArray(policy["blockedUrls"]) {
		if !isGalleryExtensionUrl(toString(u)) {
			blockedUrls = append(blockedUrls, u)
		}
	}
	c.json(200, Obj{"ok": true, "extensions": extensions, "blockedUrls": blockedUrls, "unindexedProjects": []any{}})
}

func handleAdminGetExtensionSource(c *Context) {
	hash := c.param("hash")
	path := extensionSourcePath(hash)
	if !isExtensionHash(hash) || !fsExists(path) {
		c.notFound("extension source not found")
		return
	}
	c.setHeader("Content-Type", "application/javascript; charset=utf-8")
	c.file(path)
}

func handleAdminSetExtensionPolicy(c *Context) {
	body := c.bodyJSON()
	if body["hash"] == nil || body["status"] == nil {
		c.badRequest("hash and status are required")
		return
	}
	hash := toString(body["hash"])
	status := toString(body["status"])
	if !isExtensionHash(hash) || (status != "untrusted" && status != "ignored" && status != "trusted" && status != "blocked") {
		c.badRequest("invalid extension policy")
		return
	}
	record := readExtensionRecord(hash)
	if len(objKeys(record)) == 0 {
		c.notFound("extension not found")
		return
	}
	if extensionRecordIsGallery(record) && status != "trusted" {
		c.badRequest("gallery extensions are always trusted")
		return
	}
	lockName := "extension-" + hash
	lockKey(lockName)
	record = readExtensionRecord(hash)
	record["status"] = status
	if !writeFile(extensionRecordPath(hash), record) {
		unlockKey(lockName)
		c.internalError("could not save extension policy")
		return
	}
	unlockKey(lockName)
	affectedCount := float64(0)
	if status == "blocked" {
		affected := unshareProjectsUsingHash(hash)
		affectedCount = toFloat(affected["affected"])
		if !toBool(affected["ok"]) {
			c.internalError("could not unshare every affected project")
			return
		}
	}
	c.json(200, Obj{"ok": true, "affected": affectedCount})
}

func handleAdminSetExtensionUrlPolicy(c *Context) {
	body := c.bodyJSON()
	if body["url"] == nil || body["blocked"] == nil {
		c.badRequest("URL and blocked state are required")
		return
	}
	extensionUrl := toString(body["url"])
	blocked := toBool(body["blocked"])
	if !strings.HasPrefix(extensionUrl, "https://") && !strings.HasPrefix(extensionUrl, "http://") {
		c.badRequest("an HTTP extension URL is required")
		return
	}
	if isGalleryExtensionUrl(extensionUrl) {
		c.badRequest("gallery extensions are always allowed")
		return
	}
	policy := loadExtensionPolicy()
	blockedUrls := asArray(policy["blockedUrls"])
	if blocked && !containsStr(blockedUrls, extensionUrl) {
		blockedUrls = append(blockedUrls, extensionUrl)
	}
	if !blocked {
		blockedUrls = removeStr(blockedUrls, extensionUrl)
	}
	policy["blockedUrls"] = blockedUrls
	if !saveExtensionPolicy(policy) {
		c.internalError("could not save URL policy")
		return
	}
	affectedCount := float64(0)
	if blocked {
		affected := unshareProjectsUsingUrl(extensionUrl)
		affectedCount = toFloat(affected["affected"])
		if !toBool(affected["ok"]) {
			c.internalError("could not unshare every affected project")
			return
		}
	}
	c.json(200, Obj{"ok": true, "affected": affectedCount})
}

func handleAdminIndexProjectExtensions(c *Context) {
	res := loadProject(c.param("id"))
	if !res.isOk() {
		c.notFound("project not found")
		return
	}
	project := toObj(res.unwrap())
	workId := toString(project["id"]) + "." + randomString(8)
	gzipPath := tmpDir + workId + ".json.gz"
	jsonPath := tmpDir + workId + ".json"
	if !copyStoredProjectJsonToPath(project, gzipPath) || !gunzip(gzipPath, jsonPath) {
		fsRemove(gzipPath)
		fsRemove(jsonPath)
		c.internalError("could not inspect project")
		return
	}
	body := c.bodyJSON()
	if body["sources"] == nil {
		fsRemove(gzipPath)
		fsRemove(jsonPath)
		c.badRequest("extension sources are required")
		return
	}
	collected := collectProjectExtensions(jsonPath, jsonString(body["sources"]))
	fsRemove(gzipPath)
	fsRemove(jsonPath)
	if !toBool(collected["ok"]) {
		if toString(collected["code"]) == "blocked_extension" {
			if !unshareProjectForBlockedExtension(project) {
				c.internalError("could not unshare the affected project")
				return
			}
		}
		c.json(400, collected)
		return
	}
	extensions := asArray(collected["extensions"])
	sources := toObj(collected["sources"])
	if !storeProjectExtensions(toString(project["id"]), storedProjectExtensions(project), extensions, sources) {
		c.internalError("could not store extension sources")
		return
	}
	project["extensions"] = extensions
	project["extensionsIndexed"] = true
	delete(project, "pendingExtensionUrls")
	if !saveProject(project) {
		c.internalError("could not save project extension index")
		return
	}
	c.json(200, Obj{"ok": true})
}
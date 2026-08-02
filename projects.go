package main

import (
	"fmt"
	"strings"
)

// projects.go —maps projects.osl (permission + response layer; handlers added as
// their cross-file dependencies (extensions/social) land).

func canEditProject(project Obj, c *Context) bool {
	if !c.getBool("authenticated") {
		return false
	}
	if c.getBool("isAdmin") {
		return true
	}
	return normalizeUsername(toString(project["owner"])) == normalizeUsername(c.getString("username"))
}

func canViewProject(project Obj, c *Context) bool {
	if projectIsViewable(project) {
		return true
	}
	return canEditProject(project, c)
}

func canViewProjectJson(project Obj, c *Context) bool {
	if projectPrice(project) > 0 {
		if canAccessPaidJson(project, c) {
			return true
		}
		pk := projectViewKey(project)
		if pk != "" && c.query("k", "") == pk {
			return true
		}
		return false
	}
	if project["shared"] == true || projectVisibility(project) == "unlisted" {
		return true
	}
	k := projectViewKey(project)
	if k != "" && c.query("k", "") == k {
		return true
	}
	return canEditProject(project, c)
}

func ensureViewKey(project Obj) {
	if (project["shared"] != true || projectPrice(project) > 0) && projectViewKey(project) == "" {
		project["viewKey"] = randomString(16)
		saveProject(project)
	}
}

// quotaPath mirrors projects.osl quotaPath.
func quotaPath(username string) string { return quotaDir + normalizeUsername(username) + ".json" }

func weeklyUploadedBytes(username string) float64 {
	res := readFile(quotaPath(username))
	if !res.isOk() {
		return 0
	}
	data := toObj(res.unwrap())
	total := float64(0)
	for _, it := range asArray(data["events"]) {
		event := toObj(it)
		if float64(timestamp())-toFloat(event["t"]) < quotaWindowMs {
			total += toFloat(event["bytes"])
		}
	}
	return total
}

func recordUploadBytes(username string, bytes float64) {
	events := []any{}
	res := readFile(quotaPath(username))
	if res.isOk() {
		for _, it := range asArray(toObj(res.unwrap())["events"]) {
			event := toObj(it)
			if float64(timestamp())-toFloat(event["t"]) < quotaWindowMs {
				events = append(events, event)
			}
		}
	}
	events = append(events, Obj{"t": timestamp(), "bytes": bytes})
	writeFile(quotaPath(username), Obj{"events": events})
}

func quotaDetails(username string) Obj {
	res := readFile(quotaPath(username))
	if !res.isOk() {
		return Obj{"used": float64(0), "limit": weeklyUploadQuotaBytes, "oldestEventMs": float64(0), "eventCount": float64(0)}
	}
	events := asArray(toObj(res.unwrap())["events"])
	total, oldest, count := float64(0), float64(0), float64(0)
	hasAny := false
	for _, it := range events {
		event := toObj(it)
		if float64(timestamp())-toFloat(event["t"]) < quotaWindowMs {
			total += toFloat(event["bytes"])
			count++
			if !hasAny || toFloat(event["t"]) < oldest {
				oldest = toFloat(event["t"])
				hasAny = true
			}
		}
	}
	msUntilExpiry := float64(0)
	if hasAny {
		msUntilExpiry = (oldest + quotaWindowMs) - float64(timestamp())
		if msUntilExpiry < 0 {
			msUntilExpiry = 0
		}
	}
	return Obj{"used": total, "limit": weeklyUploadQuotaBytes, "oldestEventMs": msUntilExpiry, "eventCount": count}
}

func handleGetMyQuota(c *Context) {
	username := c.getString("username")
	details := quotaDetails(username)

	rawEvents := []any{}
	res := readFile(quotaPath(username))
	if res.isOk() {
		rawEvents = asArray(toObj(res.unwrap())["events"])
	}

	dailyMap := map[float64]float64{}
	now := float64(timestamp())
	for _, it := range rawEvents {
		event := toObj(it)
		t := toFloat(event["t"])
		if now-t < quotaWindowMs {
			dayNum := float64(int(t / 86400000))
			dailyMap[dayNum] += toFloat(event["bytes"])
		}
	}

	daily := []any{}
	days := []float64{}
	for d := range dailyMap {
		days = append(days, d)
	}
	sortFloatSlice(days)
	for _, d := range days {
		daily = append(daily, Obj{"day": d, "bytes": dailyMap[d]})
	}

	c.json(200, Obj{
		"ok":            true,
		"used":          toFloat(details["used"]),
		"limit":         details["limit"],
		"windowMs":      quotaWindowMs,
		"oldestEventMs": details["oldestEventMs"],
		"eventCount":    details["eventCount"],
		"daily":         daily,
	})
}

func resetQuota(username string) bool {
	return writeFile(quotaPath(username), Obj{"events": []any{}})
}

func handleQuotaReset(c *Context) {
	if mistwarpRoturToken == "" {
		c.json(503, Obj{"ok": false, "error": "credit resets are not available right now"})
		return
	}
	userLower := normalizeUsername(c.getString("username"))
	payTo := mistwarpAccountUser()
	if payTo == "" {
		c.internalError("could not start the reset, try again")
		return
	}
	key := "mqreset_" + randomString(16)
	store := prunePurchaseKeys(loadPurchaseKeys())
	keys := toObj(store["keys"])
	keys[key] = Obj{"type": "quota_reset", "user": userLower, "at": timestamp()}
	store["keys"] = keys
	savePurchaseKeys(store)
	c.json(200, Obj{"ok": true, "key": key, "payTo": payTo, "amount": float64(20)})
}

func handleQuotaResetConfirm(c *Context) {
	if mistwarpRoturToken == "" {
		c.json(503, Obj{"ok": false, "error": "credit resets are not available right now"})
		return
	}
	user := c.getString("username")
	userLower := normalizeUsername(user)

	body := c.bodyJSON()
	key := toString(body["key"])
	if key == "" {
		c.badRequest("missing reset key")
		return
	}
	if !strings.HasPrefix(key, "mqreset_") {
		c.badRequest("invalid reset key")
		return
	}

	store := loadPurchaseKeys()
	keys := toObj(store["keys"])
	pendingRaw, ok := keys[key]
	if !ok {
		c.badRequest("this reset request expired, start again")
		return
	}
	pending := toObj(pendingRaw)
	if toString(pending["type"]) != "quota_reset" || normalizeUsername(toString(pending["user"])) != userLower {
		c.badRequest("this reset key does not match")
		return
	}

	if !verifyIncomingTransfer(userLower, key, 20) {
		c.json(402, Obj{"ok": false, "pending": true, "error": "payment not received yet, try again in a moment"})
		return
	}

	delete(keys, key)
	store["keys"] = keys
	savePurchaseKeys(store)

	if !resetQuota(userLower) {
		c.internalError("failed to reset quota")
		return
	}
	c.json(200, Obj{"ok": true, "used": float64(0), "limit": weeklyUploadQuotaBytes})
}

func handleCreateProject(c *Context) {
	body := c.bodyJSON()
	username := c.getString("username")

	title := strings.TrimSpace(toString(body["title"]))
	if title == "" {
		c.badRequest("title is required")
		return
	}
	if len(title) > 100 {
		c.badRequest("title too long")
		return
	}

	remixParent := ""
	if body["remixParent"] != nil {
		remixParent = toString(body["remixParent"])
		parentResult := loadProject(remixParent)
		if !parentResult.isOk() {
			c.badRequest("remix parent not found")
			return
		}
		if !canRemixProject(toObj(parentResult.unwrap()), c) {
			c.forbidden("this project cannot be remixed")
			return
		}
	}

	scratchOrigin := ""
	if body["scratchOrigin"] != nil {
		scratchOrigin = toString(body["scratchOrigin"])
	}

	id := generateId()
	project := Obj{
		"id":             id,
		"owner":          username,
		"title":          title,
		"description":    "",
		"instructions":   "",
		"repo":           nil,
		"jsonKey":        projectJsonKey(id),
		"viewKey":        randomString(16),
		"created":        timestamp(),
		"edited":         timestamp(),
		"sharedAt":       float64(0),
		"shared":         false,
		"lastUploadAt":   float64(0),
		"remixParent":    remixParent,
		"scratchOrigin":  scratchOrigin,
		"views":          float64(0),
		"loves":          []any{},
		"brokenhearts":   []any{},
		"assets":         []any{},
		"tags":           []any{},
	}

	if !saveProject(project) {
		c.internalError("failed to save project")
		return
	}
	upsertIndexEntry(project)
	c.json(201, Obj{"ok": true, "id": id})
}

// inspectProjectJson mirrors projects.osl: streams a project.json looking for
// targets count + md5ext list. Returns (ok, targets, md5ext).
// uploadResult mirrors the object returned by OSL uploadProjectFiles.
type uploadResult struct {
	ok             bool
	err            string
	code           string
	status         float64
	assets         []any
	uploaded       int
	gzipPath       string
	jsonBytes      float64
	storedJsonBytes float64
	assetBytes     float64
}

func uploadProjectFiles(project Obj, extractDir string) uploadResult {
	names := fsReadDir(extractDir)
	jsonPath := extractDir + "/project.json"
	if !fsExists(jsonPath) {
		return uploadResult{ok: false, err: "sb3 has no project.json", status: 400}
	}
	projectJsonBytes := fsGetSize(jsonPath)
	if projectJsonBytes > maxProjectJsonBytes {
		return uploadResult{ok: false, err: "project.json exceeds the 1 GB expanded limit", code: "project_too_large", status: 413}
	}
	valid, targets, md5ext := inspectProjectJson(jsonPath)
	if !valid {
		return uploadResult{ok: false, err: "invalid project.json", status: 400}
	}
	if targets == 0 {
		return uploadResult{ok: false, err: "invalid project.json", status: 400}
	}
	gzipPath := jsonPath + ".gz"
	if !gzipLimited(jsonPath, gzipPath, maxProjectJsonBytes, maxStoredProjectJsonBytes) {
		return uploadResult{ok: false, err: "compressed project data exceeds the 20 MB limit", code: "project_too_large", status: 413}
	}

	assetsIndex := loadAssetsIndex()
	knownAssets := toObj(assetsIndex["assets"])
	projectAssets := []any{}
	pendingUploads := []any{}
	pendingNames := Obj{}
	assetCount := 0

	for _, it := range names {
		name := toString(it)
		if name != "project.json" && isValidMd5Ext(name) {
			assetCount++
			if assetCount > int(maxAssetCount) {
				return uploadResult{ok: false, err: "too many assets", status: 400}
			}
			assetPath := extractDir + "/" + name
			if fsGetSize(assetPath) > maxAssetBytes {
				return uploadResult{ok: false, err: "an asset exceeds the 10 MB limit: " + name, code: "project_too_large", status: 413}
			}
			content := fsReadFile(assetPath)
			actualHash := md5Hex(content)
			if actualHash != md5PartOf(name) {
				return uploadResult{ok: false, err: "asset filename does not match its content: " + name, status: 400}
			}
			projectAssets = append(projectAssets, name)
			if knownAssets[name] == nil && pendingNames[name] == nil {
				pendingUploads = append(pendingUploads, Obj{"source": name, "name": name})
				pendingNames[name] = true
				knownAssets[name] = float64(len(content))
			}
		}
	}

	providedSet := Obj{}
	for _, it := range projectAssets {
		providedSet[toString(it)] = true
	}
	referencedNames := Obj{}
	for _, it := range md5ext {
		name := toString(it)
		if isValidMd5Ext(name) {
			referencedNames[name] = true
		}
	}
	referencedUnique := objKeys(referencedNames)
	for _, name := range referencedUnique {
		if providedSet[name] == nil {
			if knownAssets[name] == nil {
				return uploadResult{ok: false, err: "missing asset: " + name, status: 400}
			}
			projectAssets = append(projectAssets, name)
		}
	}
	if float64(len(projectAssets)) > maxAssetCount {
		return uploadResult{ok: false, err: "too many assets", status: 400}
	}

	assetBytes := float64(0)
	for _, it := range projectAssets {
		name := toString(it)
		if knownAssets[name] != nil {
			assetBytes += toFloat(knownAssets[name])
		}
	}
	if assetBytes > maxProjectAssetsBytes {
		return uploadResult{ok: false, err: "project assets exceed the 50 MB limit", code: "project_too_large", status: 413}
	}

	uploaded := 0
	for _, it := range pendingUploads {
		upload := toObj(it)
		source := toString(upload["source"])
		name := toString(upload["name"])
		content := fsReadFile(extractDir + "/" + source)
		if !r2Put(assetObjectKey(name), []byte(content), contentTypeForExt(extPartOf(name))) {
			return uploadResult{ok: false, err: "asset upload failed", status: 502}
		}
		knownAssets[name] = float64(len(content))
		uploaded++
	}
	if uploaded > 0 {
		saveAssetsIndex(assetsIndex)
	}

	return uploadResult{
		ok:              true,
assets:          projectAssets,
		uploaded:        uploaded,
		gzipPath:        gzipPath,
		jsonBytes:       projectJsonBytes,
		storedJsonBytes: fsGetSize(gzipPath),
		assetBytes:      assetBytes,
	}
}

func handleUploadProject(c *Context) {
	result := loadProject(c.param("id"))
	if !result.isOk() {
		c.notFound("project not found")
		return
	}
	project := toObj(result.unwrap())
	if !canEditProject(project, c) {
		c.forbidden("not your project")
		return
	}

	file, ok := c.formFile("project")
	if !ok {
		c.badRequest("missing project file")
		return
	}

	uploader := normalizeUsername(c.getString("username"))
	incomingBytes := float64(file.Size)
	if !c.getBool("isAdmin") {
		usedBytes := weeklyUploadedBytes(uploader)
		if usedBytes+incomingBytes > weeklyUploadQuotaBytes {
			usedMb := int(usedBytes / 1048576)
			limitMb := int(weeklyUploadQuotaBytes / 1048576)
			c.json(413, Obj{"ok": false, "error": fmt.Sprintf("You've used %d MB of your %d MB weekly upload budget. Usage frees up as uploads from the past 7 days age out, or you can reset it for 20 RC.", usedMb, limitMb)})
			return
		}
	}

	id := toString(project["id"])
	workId := id + "." + randomString(8)
	zipPath := tmpDir + workId + ".sb3"
	extractDir := tmpDir + workId
	if !fsWriteFileBytes(zipPath, file.Data) {
		c.internalError("failed to stage project upload")
		return
	}

	if !decompressLimited(zipPath, extractDir, maxExtractedProjectBytes, maxAssetCount+1) {
		fsRemove(zipPath)
		fsRemove(extractDir)
		c.badRequest("invalid sb3 file")
		return
	}

extensionResult := collectProjectExtensions(extractDir+"/project.json", c.formValue("extensions"))
	if !toBool(extensionResult["ok"]) {
		fsRemove(zipPath)
		fsRemove(extractDir)
		extensionError := Obj{"ok": false, "error": toString(extensionResult["error"])}
		if toString(extensionResult["code"]) != "" {
			extensionError["code"] = extensionResult["code"]
		}
		c.json(400, extensionError)
		return
	}

	uploadResult := uploadProjectFiles(project, extractDir)
	if !uploadResult.ok {
		fsRemove(zipPath)
		fsRemove(extractDir)
		errorBody := Obj{"ok": false, "error": uploadResult.err}
		if uploadResult.code != "" {
			errorBody["code"] = uploadResult.code
		}
		c.json(int(uploadResult.status), errorBody)
		return
	}

	projectExtensions := asArray(extensionResult["extensions"])
	if !storeProjectExtensions(id, storedProjectExtensions(project), projectExtensions, toObj(extensionResult["sources"])) {
		fsRemove(zipPath)
		fsRemove(extractDir)
		c.internalError("failed to store extension sources")
		return
	}

	if thumb, ok := c.formFile("thumbnail"); ok {
		if float64(thumb.Size) <= maxThumbBytes {
			fsWriteFileBytes(thumbnailPath(id), thumb.Data)
		}
	}

	project["assets"] = uploadResult.assets
	project["edited"] = timestamp()
	gzipPath := uploadResult.gzipPath
	project["sizeBytes"] = uploadResult.assetBytes + uploadResult.storedJsonBytes
	project["jsonBytes"] = uploadResult.jsonBytes
	project["storedJsonBytes"] = uploadResult.storedJsonBytes
	project["assetBytes"] = uploadResult.assetBytes
	project["jsonGzip"] = true
	project["extensions"] = projectExtensions
	project["extensionsIndexed"] = true
	delete(project, "pendingExtensionUrls")
	recordUploadBytes(uploader, incomingBytes)
	lastUploadAt := toFloat(project["lastUploadAt"])
	flushed := false
	if lastUploadAt == 0 || float64(timestamp())-lastUploadAt >= uploadDebounceMs {
		flushed = pushProjectJsonToR2(project, gzipPath)
	}
	if flushed {
		project["lastUploadAt"] = timestamp()
		project["pendingJson"] = false
	} else if stageProjectJson(project, gzipPath) {
		project["pendingJson"] = true
	} else {
		flushed = pushProjectJsonToR2(project, gzipPath)
		if flushed {
			project["lastUploadAt"] = timestamp()
			project["pendingJson"] = false
		} else {
			c.json(500, Obj{"ok": false, "error": "failed to store project data, please try saving again"})
			fsRemove(zipPath)
			fsRemove(extractDir)
			return
		}
	}

	saveProject(project)
	upsertIndexEntry(project)
	fsRemove(zipPath)
	fsRemove(extractDir)

	c.json(200, Obj{
		"ok":             true,
		"assets":         uploadResult.assets,
		"uploaded":       uploadResult.uploaded,
		"skipped":        len(uploadResult.assets) - uploadResult.uploaded,
		"projectJsonUrl": projectJsonUrlFor(project),
		"thumbUrl":       thumbUrlFor(project),
	})
}

func handleSetThumbnail(c *Context) {
	result := loadProject(c.param("id"))
	if !result.isOk() {
		c.notFound("project not found")
		return
	}
	project := toObj(result.unwrap())
	if !canEditProject(project, c) {
		c.forbidden("not your project")
		return
	}

	thumb, ok := c.formFile("thumbnail")
	if !ok {
		c.badRequest("missing thumbnail file")
		return
	}
	if float64(thumb.Size) > maxThumbBytes {
		c.badRequest("thumbnail too large")
		return
	}
	if !fsWriteFileBytes(thumbnailPath(toString(project["id"])), thumb.Data) {
		c.internalError("thumbnail save failed")
		return
	}
	project["edited"] = timestamp()
	saveProject(project)
	upsertIndexEntry(project)
	c.json(200, Obj{"ok": true, "thumbUrl": thumbUrlFor(project)})
}

func inspectProjectJson(path string) (bool, float64, []any) {
	stream := openJSONStream(path, maxProjectJsonBytes)
	if stream == nil || !stream.ok() || !stream.more() {
		if stream != nil {
			stream.close()
		}
		return false, 0, nil
	}
	root := stream.next()
	if toString(root["type"]) != "object-start" {
		stream.close()
		return false, 0, nil
	}
	var targets float64
	md5ext := []any{}
	for stream.more() {
		ev := stream.next()
		if toString(ev["type"]) == "key" {
			key := toString(ev["value"])
			if key == "targets" {
				targets++
			}
			if key == "md5ext" && stream.more() {
				value := stream.next()
				if toString(value["type"]) == "string" {
					md5ext = append(md5ext, value["value"])
					if float64(len(md5ext)) > maxAssetCount*50 {
						stream.close()
						return false, 0, nil
					}
				}
			}
		}
	}
	valid := stream.ok()
	stream.close()
	return valid, targets, md5ext
}

func sweepStagedProjects() {
	for _, f := range fsReadDir(stagingDir) {
		parts := strings.Split(f, ".")
		if len(parts) < 2 {
			continue
		}
		id := parts[1]
		res := loadProject(id)
		if res.isOk() {
			maybeFlushProject(toObj(res.unwrap()))
		}
	}
}

func stagedFlushLoop() {
	for {
		sleepMs(300)
		sweepStagedProjects()
	}
}

func handleCheckProjectAssets(c *Context) {
	result := loadProject(c.param("id"))
	if !result.isOk() {
		c.notFound("project not found")
		return
	}
	project := toObj(result.unwrap())
	if !canEditProject(project, c) {
		c.forbidden("not your project")
		return
	}
	body := c.bodyJSON()
	if body["assets"] == nil {
		c.badRequest("assets list is required")
		return
	}
	requested := asArray(body["assets"])
	if float64(len(requested)) > maxAssetCount {
		c.badRequest("too many assets")
		return
	}
	assetsIndex := loadAssetsIndex()
	knownAssets := toObj(assetsIndex["assets"])
	missing := []any{}
	for _, it := range requested {
		name := toString(it)
		if !isValidMd5Ext(name) {
			c.badRequest("invalid asset name: " + name)
			return
		}
		if knownAssets[name] == nil {
			missing = append(missing, name)
		}
	}
	c.json(200, Obj{"ok": true, "missing": missing})
}

func handleGetProject(c *Context) {
	result := loadProject(c.param("id"))
	if !result.isOk() {
		c.notFound("project not found")
		return
	}
	project := toObj(result.unwrap())
	if !canViewProject(project, c) {
		c.notFound("project not found")
		return
	}
	ensureViewKey(project)
	maybeFlushProject(project)
	c.json(200, Obj{"ok": true, "project": projectResponse(project, c)})
}

func handleGetEditorProject(c *Context) {
	result := loadProject(c.param("id"))
	if !result.isOk() {
		c.notFound("project not found")
		return
	}
	project := toObj(result.unwrap())
	if !canSeeInsideProject(project, c) {
		c.forbidden("see inside is disabled for this project")
		return
	}
	ensureViewKey(project)
	maybeFlushProject(project)
	c.json(200, Obj{"ok": true, "project": projectResponse(project, c)})
}

// pushProjectJsonToR2 places the gzipped project JSON in blob storage (local
// disk when R2 is unconfigured), then clears the staged copy.
func pushProjectJsonToR2(project Obj, gzipPath string) bool {
	if !r2PutEncoded(toString(project["jsonKey"]), fsReadFileBytes(gzipPath), "application/json", "gzip") {
		return false
	}
	fsRemove(stagingJsonPath(toString(project["id"])))
	return true
}

func stageProjectJson(project Obj, gzipPath string) bool {
	path := stagingJsonPath(toString(project["id"]))
	tmpPath := path + ".tmp." + randomString(8)
	if !copyFile(gzipPath, tmpPath) {
		return false
	}
	if !fsRename(tmpPath, path) {
		fsRemove(tmpPath)
		return false
	}
	return true
}

func maybeFlushProject(project Obj) {
	if project["pendingJson"] != true {
		return
	}
	lastUploadAt := toFloat(project["lastUploadAt"])
	if float64(timestamp())-lastUploadAt < uploadDebounceMs {
		return
	}
	path := stagingJsonPath(toString(project["id"]))
	if !fsExists(path) {
		return
	}
	gzipPath := path
	if !projectJsonIsGzip(project) {
		gzipPath = path + ".gz"
		if !gzipLimited(path, gzipPath, maxProjectJsonBytes, maxStoredProjectJsonBytes) {
			return
		}
	}
	if pushProjectJsonToR2(project, gzipPath) {
		project["lastUploadAt"] = timestamp()
		project["pendingJson"] = false
		project["jsonGzip"] = true
		saveProject(project)
	}
	if gzipPath != path {
		fsRemove(gzipPath)
	}
}

func handleGetProjectJson(c *Context) {
	result := loadProject(c.param("id"))
	if !result.isOk() {
		c.notFound("project not found")
		return
	}
	project := toObj(result.unwrap())
	if !canViewProjectJson(project, c) {
		c.notFound("project not found")
		return
	}
	if serveNotModified(c, project) {
		return
	}
	serveProjectJson(c, project)
}

func handleUpdateProject(c *Context) {
	result := loadProject(c.param("id"))
	if !result.isOk() {
		c.notFound("project not found")
		return
	}
	project := toObj(result.unwrap())
	if !canEditProject(project, c) {
		c.forbidden("not your project")
		return
	}

	body := c.bodyJSON()
	if body["title"] != nil {
		title := strings.TrimSpace(toString(body["title"]))
		if title == "" || len(title) > 100 {
			c.badRequest("invalid title")
			return
		}
		project["title"] = title
	}
	if body["description"] != nil {
		description := toString(body["description"])
		if len(description) > 5000 {
			c.badRequest("description too long")
			return
		}
		project["description"] = description
	}
	if body["instructions"] != nil {
		instructions := toString(body["instructions"])
		if len(instructions) > 5000 {
			c.badRequest("instructions too long")
			return
		}
		project["instructions"] = instructions
	}
	if body["notes"] != nil {
		notes := toString(body["notes"])
		if len(notes) > 5000 {
			c.badRequest("notes too long")
			return
		}
		project["notes"] = notes
	}
	if body["credits"] != nil {
		incoming := asArray(body["credits"])
		if float64(len(incoming)) > 50 {
			c.badRequest("too many credits")
			return
		}
		credits := []any{}
		for _, it := range incoming {
			entry := toObj(it)
			who := strings.TrimSpace(toString(entry["who"]))
			role := strings.TrimSpace(toString(entry["role"]))
			if who != "" && len(who) <= 60 && len(role) <= 120 {
				credits = append(credits, Obj{"who": who, "role": role})
			}
		}
		project["credits"] = credits
	}
	if body["commentsOff"] != nil {
		project["commentsOff"] = toBool(body["commentsOff"])
	}
	if body["remixable"] != nil {
		project["remixable"] = !toBool(body["remixable"])
	}
	if body["seeInside"] != nil {
		project["seeInside"] = !toBool(body["seeInside"])
	}
	if body["price"] != nil {
		price := toFloat(body["price"])
		if price < 0 || price > 1000000 {
			c.badRequest("invalid price")
			return
		}
		oldPrice := projectPrice(project)
		project["price"] = price
		if price > 0 && oldPrice <= 0 {
			project["viewKey"] = randomString(16)
		}
	}
	if body["tags"] != nil {
		incoming := asArray(body["tags"])
		tags := []any{}
		for _, it := range incoming {
			if float64(len(tags)) < 10 {
				t := normalizeTag(toString(it))
				if t != "" && !containsStr(tags, t) {
					tags = append(tags, t)
				}
			}
		}
		project["tags"] = tags
	}

	project["edited"] = timestamp()
	saveProject(project)
	upsertIndexEntry(project)
	c.json(200, Obj{"ok": true, "project": projectResponse(project, c)})
}

func collectAssetsUsedBy(excludeId string) Obj {
	used := Obj{}
	for _, it := range fsReadDir(projectsDir) {
		res := readFile(projectsDir + toString(it))
		if res.isOk() {
			p := toObj(res.unwrap())
			if toString(p["id"]) != excludeId && p["assets"] != nil {
				for _, a := range asArray(p["assets"]) {
					used[toString(a)] = true
				}
			}
		}
	}
	return used
}

func deleteOrphanedAssets(project Obj) {
	if project["assets"] == nil {
		return
	}
	myAssets := asArray(project["assets"])
	if len(myAssets) == 0 {
		return
	}
	usedByOthers := collectAssetsUsedBy(toString(project["id"]))
	assetsIndex := loadAssetsIndex()
	knownAssets := toObj(assetsIndex["assets"])
	indexChanged := false
	for _, it := range myAssets {
		name := toString(it)
		if usedByOthers[name] == nil {
			r2Remove(assetObjectKey(name))
			if knownAssets[name] != nil {
				delete(knownAssets, name)
				indexChanged = true
			}
		}
	}
	if indexChanged {
		saveAssetsIndex(assetsIndex)
	}
}

func purgeProject(project Obj) {
	id := toString(project["id"])
	removeProjectExtensionUsage(id, storedProjectExtensions(project))
	deleteProjectFile(id)
	removeFromIndex(id)
	fsRemove(stagingJsonPath(id))
	fsRemove(thumbnailPath(id))
	fsRemove(commentsDir + "project-" + id + ".json")
	fsRemove(commentsDir + "project-" + id + ".jsonl")
	r2Remove(toString(project["jsonKey"]))
	deleteOrphanedAssets(project)
}

func handleDeleteProject(c *Context) {
	result := loadProject(c.param("id"))
	if !result.isOk() {
		c.notFound("project not found")
		return
	}
	project := toObj(result.unwrap())
	if !canEditProject(project, c) {
		c.forbidden("not your project")
		return
	}
	if project["shared"] == true && !c.getBool("isAdmin") {
		c.badRequest("unshare the project before deleting it")
		return
	}
	purgeProject(project)
	c.json(200, Obj{"ok": true})
}

func handlePublishProject(c *Context) {
	result := loadProject(c.param("id"))
	if !result.isOk() {
		c.notFound("project not found")
		return
	}
	project := toObj(result.unwrap())
	if !canEditProject(project, c) {
		c.forbidden("not your project")
		return
	}

	blockedExtension := projectBlockedExtension(project)
	if blockedExtension != "" {
		c.badRequest("remove the blocked extension before sharing: " + blockedExtension)
		return
	}

	everUploaded := toFloat(project["lastUploadAt"]) > 0
	if project["pendingJson"] == true {
		everUploaded = true
	}
	if !everUploaded {
		c.badRequest("upload the project before publishing")
		return
	}

	firstShare := toFloat(project["sharedAt"]) == 0
	project["shared"] = true
	project["visibility"] = "public"
	if firstShare {
		project["sharedAt"] = timestamp()
	}
	project["edited"] = timestamp()
	saveProject(project)
	upsertIndexEntry(project)
	if firstShare {
		addActivity(toString(project["owner"]), "share", Obj{"projectId": project["id"], "projectTitle": project["title"]})
	}
	c.json(200, Obj{"ok": true, "project": projectResponse(project, c)})
}

func handleUnpublishProject(c *Context) {
	result := loadProject(c.param("id"))
	if !result.isOk() {
		c.notFound("project not found")
		return
	}
	project := toObj(result.unwrap())
	if !canEditProject(project, c) {
		c.forbidden("not your project")
		return
	}

	project["shared"] = false
	project["visibility"] = "private"
	project["edited"] = timestamp()
	project["viewKey"] = randomString(16)
	saveProject(project)
	upsertIndexEntry(project)
	profile := loadProfile(toString(project["owner"]))
	if toString(profile["featuredProject"]) == toString(project["id"]) {
		profile["featuredProject"] = ""
		saveProfile(profile)
	}
	c.json(200, Obj{"ok": true, "project": projectResponse(project, c)})
}

func handleViewProject(c *Context) {
	result := loadProject(c.param("id"))
	if !result.isOk() {
		c.notFound("project not found")
		return
	}
	project := toObj(result.unwrap())
	if !projectIsViewable(project) {
		c.json(200, Obj{"ok": true})
		return
	}
	viewer := normalizeUsername(c.getString("username"))
	if viewer != "" && viewer == normalizeUsername(toString(project["owner"])) {
		c.json(200, Obj{"ok": true})
		return
	}
	dedupKey := toString(project["id"]) + "|" + viewer
	if viewer == "" {
		dedupKey = toString(project["id"]) + "|ip:" + c.ip()
	}
	if viewDedup.has(dedupKey) {
		c.json(200, Obj{"ok": true})
		return
	}
	viewDedup.set(dedupKey, true)
	project["views"] = toFloat(project["views"]) + 1

	history := Obj{}
	if project["viewHistory"] != nil {
		history = toObj(project["viewHistory"])
	}
	today := float64(int(float64(timestamp()) / 86400000))
	dayKey := todayIndex()
	dayCount := float64(0)
	if history[dayKey] != nil {
		dayCount = toFloat(history[dayKey])
	}
	pruned := Obj{}
	for k, v := range history {
		if today-toFloat(k) < 31 {
			pruned[k] = v
		}
	}
	pruned[dayKey] = dayCount + 1
	project["viewHistory"] = pruned
	saveProject(project)
	flushKey := toString(project["id"])
	if !viewIndexFill.has(flushKey) {
		viewIndexFill.set(flushKey, true)
		upsertIndexEntry(project)
	}
	c.json(200, Obj{"ok": true})
}

func handleReactProject(c *Context) {
	body := c.bodyJSON()
	reactionType := toString(body["type"])
	if !isValidReaction(reactionType) {
		c.badRequest("invalid reaction")
		return
	}
	result := loadProject(c.param("id"))
	if !result.isOk() {
		c.notFound("project not found")
		return
	}
	project := toObj(result.unwrap())
	if !canViewProject(project, c) {
		c.notFound("project not found")
		return
	}
	if !canInteractProject(project, c) {
		c.forbidden("buy this project to react to it")
		return
	}

	viewer := normalizeUsername(c.getString("username"))
	loves := asArray(project["loves"])
	var broken []any
	if project["brokenhearts"] != nil {
		broken = asArray(project["brokenhearts"])
	}
	hadHeart := containsStr(loves, viewer)
	hadBroken := containsStr(broken, viewer)
	loves = removeStr(loves, viewer)
	broken = removeStr(broken, viewer)
	addedHeart := false
	if reactionType == "heart" && !hadHeart {
		loves = append(loves, viewer)
		addedHeart = true
	}
	if reactionType == "brokenheart" && !hadBroken {
		broken = append(broken, viewer)
	}
	project["loves"] = loves
	project["brokenhearts"] = broken
	saveProject(project)
	upsertIndexEntry(project)

	profile := loadProfile(viewer)
	mine := []any{}
	if profile["loves"] != nil {
		mine = asArray(profile["loves"])
	}
	mine = removeStr(mine, toString(project["id"]))
	if containsStr(loves, viewer) {
		mine = append(mine, toString(project["id"]))
	}
	profile["loves"] = mine
	saveProfile(profile)

	if addedHeart {
		addNotification(toString(project["owner"]), viewer, "love", Obj{"projectId": project["id"], "projectTitle": project["title"]})
		addActivity(viewer, "love", Obj{"projectId": project["id"], "projectTitle": project["title"], "projectOwner": project["owner"]})
	}

	c.json(200, Obj{"ok": true, "hearts": float64(len(loves)), "brokenHearts": float64(len(broken))})
}

func sharedIndexEntries() []any {
	projects := toObj(loadIndex()["projects"])
	keys := objKeys(projects)
	banData := cachedBans()
	bans := toObj(banData["bans"])
	entries := []any{}
	for _, k := range keys {
		entry := toObj(projects[k])
		if entry["shared"] == true && bans[normalizeUsername(toString(entry["owner"]))] == nil {
			entries = append(entries, entry)
		}
	}
	return entries
}

func handleExplore(c *Context) {
	entries := sharedIndexEntries()

	q := strings.ToLower(strings.TrimSpace(c.queryDefault("q", "")))
	tag := strings.ToLower(strings.TrimSpace(c.queryDefault("tag", "")))
	if strings.HasPrefix(q, "#") {
		tag = normalizeTag(q)
		q = ""
	}

	if tag != "" {
		filtered := []any{}
		for _, it := range entries {
			entry := toObj(it)
			var tags []any
			if entry["tags"] != nil {
				tags = asArray(entry["tags"])
			}
			if containsStr(tags, tag) {
				filtered = append(filtered, entry)
			}
		}
		entries = filtered
	} else if q != "" {
		filtered := []any{}
		for _, it := range entries {
			entry := toObj(it)
			match := strings.Contains(strings.ToLower(toString(entry["title"])), q) || strings.Contains(strings.ToLower(toString(entry["owner"])), q)
			if !match && entry["tags"] != nil {
				for _, t := range asArray(entry["tags"]) {
					if strings.Contains(toString(t), q) {
						match = true
					}
				}
			}
			if match {
				filtered = append(filtered, entry)
			}
		}
		entries = filtered
	}

	sort := c.queryDefault("sort", "recent")
	if sort == "loved" {
		sortObjBy(entries, "loveCount", "descending")
	} else if sort == "trending" {
		for _, it := range entries {
			entry := toObj(it)
			weekViews := float64(0)
			if entry["weekViews"] != nil {
				weekViews = toFloat(entry["weekViews"])
			}
			entry["score"] = weekViews
		}
		sortObjBy(entries, "score", "descending")
	} else {
		sortObjBy(entries, "sharedAt", "descending")
	}

	total := float64(len(entries))
	offset := clampNumber(float64(c.queryInt("offset", 0)), 0, total)
	limit := clampNumber(float64(c.queryInt("limit", 16)), 1, 50)

	c.json(200, Obj{"ok": true, "total": total, "projects": paginate(entries, offset, limit)})
}

func handleLeaderboard(c *Context) {
	entries := sharedIndexEntries()
	totals := map[string]Obj{}
	for _, it := range entries {
		entry := toObj(it)
		owner := normalizeUsername(toString(entry["owner"]))
		if owner != "" {
			row, ok := totals[owner]
			if !ok {
				row = Obj{"username": entry["owner"], "loves": float64(0), "views": float64(0), "projects": float64(0)}
				totals[owner] = row
			}
			row["loves"] = toFloat(row["loves"]) + toFloat(entry["loveCount"])
			row["views"] = toFloat(row["views"]) + toFloat(entry["views"])
			row["projects"] = toFloat(row["projects"]) + 1
		}
	}
	rows := []any{}
	for _, row := range totals {
		rows = append(rows, row)
	}
	by := c.queryDefault("by", "loves")
	if by != "views" {
		by = "loves"
	}
	sortObjBy(rows, by, "descending")
	limit := clampNumber(float64(c.queryInt("limit", 15)), 1, 50)
	c.json(200, Obj{"ok": true, "by": by, "users": paginate(rows, 0, limit)})
}

func handleGetRemixes(c *Context) {
	id := c.param("id")
	entries := sharedIndexEntries()
	remixes := []any{}
	for _, it := range entries {
		entry := toObj(it)
		if toString(entry["remixParent"]) == id {
			remixes = append(remixes, entry)
		}
	}
	sortObjBy(remixes, "sharedAt", "descending")
	c.json(200, Obj{"ok": true, "remixes": remixes})
}

func handleGetRemixTree(c *Context) {
	id := c.param("id")
	result := loadProject(id)
	if !result.isOk() {
		c.notFound("project not found")
		return
	}
	project := toObj(result.unwrap())
	if !canViewProject(project, c) {
		c.notFound("project not found")
		return
	}

	entries := sharedIndexEntries()
	byId := Obj{}
	childrenByParent := Obj{}
	for _, it := range entries {
		entry := toObj(it)
		byId[toString(entry["id"])] = entry
		parentKey := toString(entry["remixParent"])
		if parentKey != "" {
			kids := []any{}
			if childrenByParent[parentKey] != nil {
				kids = asArray(childrenByParent[parentKey])
			}
			kids = append(kids, entry["id"])
			childrenByParent[parentKey] = kids
		}
	}
	if byId[id] == nil {
		byId[id] = indexEntryFromProject(project)
	}

	rootId := id
	hops := 0
	walking := true
	for walking && hops < 100 {
		current := toObj(byId[rootId])
		parent := toString(current["remixParent"])
		if parent != "" && byId[parent] != nil && parent != rootId {
			rootId = parent
		} else {
			walking = false
		}
		hops++
	}

	nodes := []any{}
	order := []string{rootId}
	seen := Obj{}
	seen[rootId] = true
	cursor := 1
	for cursor <= len(order) && len(nodes) < 500 {
		currentId := order[cursor-1]
		nodes = append(nodes, byId[currentId])
		if childrenByParent[currentId] != nil {
			kids := asArray(childrenByParent[currentId])
			for _, kid := range kids {
				entryId := toString(kid)
				if seen[entryId] == nil {
					seen[entryId] = true
					order = append(order, entryId)
				}
			}
		}
		cursor++
	}

	c.json(200, Obj{"ok": true, "root": rootId, "nodes": nodes})
}

func handleRemixProject(c *Context) {
	result := loadProject(c.param("id"))
	if !result.isOk() {
		c.notFound("project not found")
		return
	}
	parent := toObj(result.unwrap())
	if !canViewProject(parent, c) {
		c.notFound("project not found")
		return
	}
	if !canRemixProject(parent, c) {
		c.forbidden("this project cannot be remixed")
		return
	}

	username := c.getString("username")
	id := generateId()

	copiedJsonPath := tmpDir + id + "." + randomString(8) + ".json.gz"
	if !copyStoredProjectJsonToPath(parent, copiedJsonPath) {
		c.internalError("could not copy the original project, try again")
		return
	}
	if !r2PutEncoded(projectJsonKey(id), fsReadFileBytes(copiedJsonPath), "application/json", "gzip") {
		fsRemove(copiedJsonPath)
		c.internalError("could not copy the original project, try again")
		return
	}
	copiedJsonBytes := fsGetSize(copiedJsonPath)
	fsRemove(copiedJsonPath)

	project := Obj{
		"id":              id,
		"owner":           username,
		"title":           toString(parent["title"]) + " remix",
		"description":     "",
		"instructions":    "",
		"repo":            nil,
		"jsonKey":         projectJsonKey(id),
		"viewKey":         randomString(16),
		"created":         timestamp(),
		"edited":          timestamp(),
		"sharedAt":        float64(0),
		"shared":          false,
		"lastUploadAt":    timestamp(),
		"jsonGzip":        true,
		"storedJsonBytes": copiedJsonBytes,
		"remixParent":     parent["id"],
		"scratchOrigin":   "",
		"views":           float64(0),
		"loves":           []any{},
		"brokenhearts":    []any{},
		"assets":          parent["assets"],
		"sizeBytes":       projectSizeBytes(parent),
	}
	if parent["jsonBytes"] != nil {
		project["jsonBytes"] = parent["jsonBytes"]
	}
	if parent["assetBytes"] != nil {
		project["assetBytes"] = parent["assetBytes"]
	}
	if parent["extensions"] != nil {
		project["extensions"] = parent["extensions"]
	}
	if parent["extensionsIndexed"] != nil {
		project["extensionsIndexed"] = parent["extensionsIndexed"]
	}

	if !storeProjectExtensions(id, []any{}, storedProjectExtensions(project), Obj{}) {
		r2Remove(toString(project["jsonKey"]))
		c.internalError("could not index project extensions")
		return
	}
	saveProject(project)
	upsertIndexEntry(project)

	addNotification(toString(parent["owner"]), username, "remix", Obj{"projectId": id, "projectTitle": parent["title"]})
	addActivity(username, "remix", Obj{"projectId": id, "projectTitle": toString(parent["title"]) + " remix", "parentId": toString(parent["id"]), "parentTitle": parent["title"]})

	c.json(201, Obj{
		"ok":             true,
		"id":             id,
		"projectJsonUrl": projectJsonUrlFor(project),
		"assetsBase":     assetsBaseUrl(),
	})
}

func projectResponse(project Obj, c *Context) Obj {
	loves := asArray(project["loves"])
	broken := asArray(project["brokenhearts"])
	viewer := normalizeUsername(c.getString("username"))
	myReaction := ""
	if containsStr(loves, viewer) {
		myReaction = "heart"
	}
	if containsStr(broken, viewer) {
		myReaction = "brokenheart"
	}
	notes := toString(project["notes"])
	credits := asArray(project["credits"])
	commentsOff := false
	if project["commentsOff"] == true {
		commentsOff = true
	}
	tags := asArray(project["tags"])

	isOwner := canEditProject(project, c)
	price := projectPrice(project)
	bought := hasBought(project, viewer)
	hasContent := toFloat(project["lastUploadAt"]) > 0
	hasContent = hasContent || toBool(project["pendingJson"])

	jsonBase := projectJsonBaseUrl(project)
	jsonUrl := jsonBase
	if project["shared"] != true && projectViewKey(project) != "" {
		jsonUrl = jsonBase + "?k=" + projectViewKey(project)
	}
	locked := false
	if price > 0 {
		if isOwner || bought {
			jsonUrl = jsonBase + "?k=" + projectViewKey(project)
		} else {
			jsonUrl = ""
			locked = true
		}
	}
	if !hasContent {
		jsonUrl = ""
	}

	response := Obj{
		"id":              project["id"],
		"owner":           project["owner"],
		"title":           project["title"],
		"tags":            tags,
		"description":     project["description"],
		"instructions":    project["instructions"],
		"notes":           notes,
		"credits":         credits,
		"repo":            project["repo"],
		"projectJsonUrl":  jsonUrl,
		"assetsBase":      assetsBaseUrl(),
		"thumbUrl":        thumbUrlFor(project),
		"created":         project["created"],
		"edited":          project["edited"],
		"sharedAt":        project["sharedAt"],
		"shared":          project["shared"],
		"visibility":      projectVisibility(project),
		"price":           price,
		"locked":          locked,
		"bought":          bought,
		"saved":           isInLibrary(viewer, toString(project["id"])),
		"hasContent":      hasContent,
		"remixable":       projectRemixable(project),
		"canRemix":        canRemixProject(project, c),
		"seeInside":       projectSeeInside(project),
		"canSeeInside":    canSeeInsideProject(project, c),
		"lastUploadAt":    project["lastUploadAt"],
		"remixParent":     project["remixParent"],
		"scratchOrigin":   project["scratchOrigin"],
		"views":           project["views"],
		"loveCount":       float64(len(loves)),
		"brokenHeartCount": float64(len(broken)),
		"myReaction":      myReaction,
		"commentsOff":     commentsOff,
		"sizeBytes":       projectSizeBytes(project),
		"isOwner":         isOwner,
	}
	if project["jsonBytes"] != nil {
		response["jsonBytes"] = project["jsonBytes"]
	}
	if project["storedJsonBytes"] != nil {
		response["storedJsonBytes"] = project["storedJsonBytes"]
	}
	if project["assetBytes"] != nil {
		response["assetBytes"] = project["assetBytes"]
	}
	if !locked && hasContent {
		trustedExtensions := projectTrustedExtensions(project)
		if len(trustedExtensions) > 0 {
			response["trustedExtensions"] = trustedExtensions
		}
	}
	if isOwner {
		viewHistory := Obj{}
		if project["viewHistory"] != nil {
			viewHistory = toObj(project["viewHistory"])
		}
		saleHistory := Obj{}
		if project["saleHistory"] != nil {
			saleHistory = toObj(project["saleHistory"])
		}
		response["analytics"] = Obj{
			"revenue":     projectRevenue(project),
			"buyers":      projectBuyers(project),
			"saves":       projectSaves(project),
			"viewHistory": viewHistory,
			"saleHistory": saleHistory,
		}
	}
	return response
}

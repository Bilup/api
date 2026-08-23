package main

import "strings"

// storage.osl

func ensureDirectories() bool {
	result := fsMkdirAll(projectsDir)
	result = fsMkdirAll(usersDir) && result
	result = fsMkdirAll(sessionsDir) && result
	result = fsMkdirAll(commentsDir) && result
	result = fsMkdirAll(reportsDir) && result
	result = fsMkdirAll(notificationsDir) && result
	result = fsMkdirAll(activityDir) && result
	result = fsMkdirAll(userSettingsDir) && result
	result = fsMkdirAll(tmpDir) && result
	result = fsMkdirAll(stagingDir) && result
	result = fsMkdirAll(thumbnailsDir) && result
	result = fsMkdirAll(quotaDir) && result
	result = fsMkdirAll(agreementAcceptDir) && result
	result = fsMkdirAll(extensionsDir) && result
	result = fsMkdirAll(releasesDir) && result
	result = fsMkdirAll(communityDir) && result
	return result
}

func projectSizeBytes(project Obj) float64 {
	if v, ok := project["sizeBytes"]; ok && v != nil {
		return toFloat(v)
	}
	return 0
}

func weekViewsOf(project Obj) float64 {
	h, ok := project["viewHistory"]
	if !ok || h == nil {
		return 0
	}
	history, ok := h.(map[string]any)
	if !ok {
		return 0
	}
	today := float64(nowMs()) / 86400000
	total := 0.0
	for _, k := range objKeys(history) {
		if today-toFloat(k) < 7 {
			total += toFloat(history[k])
		}
	}
	return total
}

func loadAssetsIndex() Obj {
	res := readFile(assetsIndexFile)
	if !res.isOk() {
		return Obj{"assets": Obj{}}
	}
	return res.data.(Obj)
}

func saveAssetsIndex(index Obj) bool { return writeFile(assetsIndexFile, index) }

func readFile(path string) *result {
	if !fsExists(path) {
		return &result{err: "file not found"}
	}
	content := fsReadFile(path)
	parsed := tryParseJSON(content, "object")
	if parsed.isErr() {
		return &result{err: "invalid json"}
	}
	return parsed
}

func writeFile(path string, data any) bool {
	tmpPath := path + ".tmp." + randomString(8)
	if !fsWriteFile(tmpPath, jsonString(data)) {
		return false
	}
	if !fsRename(tmpPath, path) {
		fsRemove(tmpPath)
		return false
	}
	return true
}

func loadIndex() Obj {
	res := readFile(indexFile)
	if res.isErr() {
		return Obj{"projects": Obj{}}
	}
	return res.data.(Obj)
}

func saveIndex(index Obj) bool { return writeFile(indexFile, index) }

func indexEntryFromProject(project Obj) Obj {
	loves := arrFrom(project, "loves")
	broken := []any{}
	if v, ok := project["brokenhearts"]; ok {
		broken = asArray(v)
	}
	tags := []any{}
	if v, ok := project["tags"]; ok && v != nil {
		tags = arrAs(v)
	}
	return Obj{
		"id":              project["id"],
		"owner":           project["owner"],
		"title":           project["title"],
		"thumbUrl":        thumbUrlFor(project),
		"views":           project["views"],
		"loveCount":       float64(len(loves)),
		"brokenHeartCount": float64(len(broken)),
		"remixParent":     project["remixParent"],
		"shared":          project["shared"],
		"visibility":      projectVisibility(project),
		"price":           projectPrice(project),
		"revenue":         projectRevenue(project),
		"created":         project["created"],
		"edited":          project["edited"],
		"sharedAt":        project["sharedAt"],
		"sizeBytes":       projectSizeBytes(project),
		"weekViews":       weekViewsOf(project),
		"tags":            tags,
	}
}

func refreshIndexUrls() bool {
	index := loadIndex()
	projects := index["projects"].(map[string]any)
	for _, k := range objKeys(projects) {
		entry := projects[k].(map[string]any)
		entry["thumbUrl"] = thumbUrlFor(entry)
	}
	return saveIndex(index)
}

func upsertIndexEntry(project Obj) bool {
	index := loadIndex()
	projects := index["projects"].(map[string]any)
	projects[toString(project["id"])] = indexEntryFromProject(project)
	return saveIndex(index)
}

func removeFromIndex(id string) bool {
	index := loadIndex()
	projects := index["projects"].(map[string]any)
	if objHas(projects, id) {
		delete(projects, id)
		return saveIndex(index)
	}
	return true
}

func loadProject(id string) *result {
	if !isSafePathPart(id) {
		return &result{err: "invalid project id"}
	}
	res := readFile(projectsDir + id + ".json")
	if res.isErr() {
		return &result{err: "project not found"}
	}
	return &result{ok: true, data: res.data}
}

func saveProject(project Obj) bool {
	id := toString(project["id"])
	if !isSafePathPart(id) {
		return false
	}
	return writeFile(projectsDir+id+".json", project)
}

func deleteProjectFile(id string) bool {
	if !isSafePathPart(id) {
		return false
	}
	return fsRemove(projectsDir + id + ".json")
}

func defaultProfile(username string) Obj {
	return Obj{
		"username":        username,
		"bio":             "",
		"featuredProject": "",
		"followers":       []any{},
		"following":       []any{},
		"loves":           []any{},
		"created":         nowMs(),
	}
}

func loadProfile(username string) Obj {
	key := normalizeUsername(username)
	if !isSafePathPart(key) {
		return defaultProfile(username)
	}
	res := readFile(usersDir + key + ".json")
	if res.isErr() {
		return defaultProfile(username)
	}
	return res.data.(Obj)
}

func saveProfile(profile Obj) bool {
	key := normalizeUsername(toString(profile["username"]))
	if !isSafePathPart(key) {
		return false
	}
	return writeFile(usersDir+key+".json", profile)
}

func loadActivity(username string) []any {
	key := normalizeUsername(username)
	if !isSafePathPart(key) {
		return []any{}
	}
	res := readFile(activityDir + key + ".json")
	if res.isErr() {
		return []any{}
	}
	data := res.data.(Obj)
	items, _ := data["items"].([]any)
	return items
}

func addActivity(username, activityType string, extra Obj) {
	key := normalizeUsername(username)
	if !isSafePathPart(key) {
		return
	}
	item := Obj{"type": activityType, "actor": key, "created": nowMs()}
	for _, k := range objKeys(extra) {
		item[k] = extra[k]
	}
	items := []any{item}
	existing := loadActivity(key)
	for _, e := range existing {
		if len(items) < 50 {
			items = append(items, e)
		}
	}
	writeFile(activityDir+key+".json", Obj{"items": items})
}

func loadCommentsLegacy(key string) []any {
	res := readFile(commentsDir + key + ".json")
	if res.isErr() {
		return []any{}
	}
	data := res.data.(Obj)
	comments, _ := data["comments"].([]any)
	return comments
}

func loadComments(key string) []any {
	if !isSafePathPart(key) {
		return []any{}
	}
	path := commentsDir + key + ".jsonl"
	if !fsExists(path) {
		return loadCommentsLegacy(key)
	}
	lines := strings.Split(fsReadFile(path), "\n")
	out := []any{}
	for _, each := range lines {
		line := strings.TrimSpace(each)
		if line != "" {
			parsed := tryParseJSON(line, "object")
			if !parsed.isErr() {
				out = append(out, parsed.data)
			}
		}
	}
	return out
}

func saveComments(key string, comments []any) bool {
	if !isSafePathPart(key) {
		return false
	}
	out := ""
	for _, each := range comments {
		out += jsonString(each) + "\n"
	}
	ok := fsWriteFile(commentsDir+key+".jsonl", out)
	fsRemove(commentsDir + key + ".json")
	return ok
}

func appendComment(key string, comment Obj) bool {
	if !isSafePathPart(key) {
		return false
	}
	return fsAppendToFile(commentsDir+key+".jsonl", jsonString(comment)+"\n")
}

func arrAs(v any) []any {
	arr, ok := v.([]any)
	if ok {
		return arr
	}
	return []any{}
}
package main

import "strings"

// report-evidence.go — maps report-evidence.osl.

func evidenceDirFor(id string) string { return reportEvidenceDir + id + "/" }

func loadEvidenceMeta(id string) Obj {
	res := readFile(evidenceDirFor(id) + "meta.json")
	if !res.isOk() {
		return Obj{}
	}
	return toObj(res.unwrap())
}

func readAssetBytes(name string) string {
	local := localBlobPath(assetObjectKey(name))
	if fsExists(local) {
		return fsReadFile(local)
	}
	if r2Local {
		return ""
	}
	direct := r2DirectUrl(assetObjectKey(name))
	if direct == "" {
		return ""
	}
	fetched := requestsGet(direct, map[string]any{})
	if fetched.success && fetched.status == 200 {
		return fetched.body
	}
	return ""
}

func snapshotProjectEvidence(projectId string) {
	if !isSafePathPart(projectId) {
		return
	}
	dir := evidenceDirFor(projectId)
	if fsExists(dir + "meta.json") {
		return
	}
	res := loadProject(projectId)
	if !res.isOk() {
		return
	}
	project := toObj(res.unwrap())
	fsMkdirAll(dir + "assets/")
	project["evidenceKey"] = randomString(16)
	project["snapshotAt"] = timestamp()
	writeFile(dir+"meta.json", project)

	if copyStoredProjectJsonToPath(project, dir+"project.json.gz") {
		for _, n := range asArray(project["assets"]) {
			name := toString(n)
			if bytes := readAssetBytes(name); bytes != "" {
				fsWriteFileBytes(dir+"assets/"+name, []byte(bytes))
			}
		}
	}

	thumb := thumbnailPath(projectId)
	if fsExists(thumb) {
		fsWriteFileBytes(dir+"thumbnail.png", fsReadFileBytes(thumb))
	}
}

func projectHasOpenReports(projectId string) bool {
	for _, it := range loadReports() {
		r := toObj(it)
		if toString(r["type"]) == "project" && toString(r["target"]) == projectId && !toBool(r["resolved"]) {
			return true
		}
	}
	return false
}

func removeEvidenceDir(id string) {
	dir := evidenceDirFor(id)
	assetsDir := dir + "assets/"
	if fsExists(assetsDir) {
		for _, name := range fsReadDir(assetsDir) {
			fsRemove(assetsDir + name)
		}
		fsRemove(assetsDir)
	}
	fsRemove(dir + "project.json")
	fsRemove(dir + "project.json.gz")
	fsRemove(dir + "meta.json")
	fsRemove(dir + "thumbnail.png")
	fsRemove(dir)
}

func maybeClearProjectEvidence(projectId string) {
	if !isSafePathPart(projectId) {
		return
	}
	if !fsExists(evidenceDirFor(projectId) + "meta.json") {
		return
	}
	if !projectHasOpenReports(projectId) {
		removeEvidenceDir(projectId)
	}
}

func handleGetEvidenceJson(c *Context) {
	id := c.param("id")
	if !isSafePathPart(id) {
		c.notFound("not found")
		return
	}
	meta := loadEvidenceMeta(id)
	k := ""
	if meta["evidenceKey"] != nil {
		k = toString(meta["evidenceKey"])
	}
	if k == "" || c.query("k", "") != k {
		c.notFound("not found")
		return
	}
	path := evidenceDirFor(id) + "project.json.gz"
	compressed := true
	if !fsExists(path) {
		path = evidenceDirFor(id) + "project.json"
		compressed = false
	}
	if !fsExists(path) {
		c.notFound("not found")
		return
	}
	c.setHeader("Content-Type", "application/json")
	c.setHeader("Cache-Control", "no-store")
	if compressed {
		c.setHeader("Content-Encoding", "gzip")
	}
	c.file(path)
}

func handleGetEvidenceAsset(c *Context) {
	id := c.param("id")
	name := c.param("name")
	if !isSafePathPart(id) || !isValidMd5Ext(name) {
		c.notFound("not found")
		return
	}
	path := evidenceDirFor(id) + "assets/" + name
	if !fsExists(path) {
		c.notFound("not found")
		return
	}
	c.setHeader("Content-Type", contentTypeForKey(name))
	c.setHeader("Cache-Control", "no-store")
	c.file(path)
}

func handleGetReportEvidence(c *Context) {
	id := c.param("id")
	if !isSafePathPart(id) {
		c.notFound("not found")
		return
	}
	meta := loadEvidenceMeta(id)
	if meta["evidenceKey"] == nil {
		c.json(200, Obj{"ok": true, "exists": false})
		return
	}
	key := toString(meta["evidenceKey"])
	base := appURL + "/blobs/evidence/" + id
	c.json(200, Obj{
		"ok":            true,
		"exists":        true,
		"config":        meta,
		"projectJsonUrl": base + "project.json?k=" + key,
		"assetsBase":    base + "/assets",
	})
}

var _ = strings.TrimSpace
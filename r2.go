package main

import (
	"regexp"
	"strings"
)

// r2.osl: project json / blob storage (local disk path; remote R2 writes pending).

func projectJsonKey(id string) string          { return "projects/" + id + "/project.json" }
func assetObjectKey(md5ext string) string      { return "assets/" + md5ext }
func stagingJsonPath(id string) string         { return stagingDir + id + ".json" }
func projectJsonIsGzip(project Obj) bool {
	return objHas(project, "jsonGzip") && project["jsonGzip"] == true
}
func thumbnailPath(id string) string { return thumbnailsDir + id + ".png" }

func thumbUrlFor(project Obj) string { return thumbUrlForChain(project, 0) }

func thumbUrlForChain(project Obj, hops int) string {
	id := toString(project["id"])
	if fsExists(thumbnailPath(id)) {
		return appURL + "/thumbnails/" + id + ".png?v=" + toString(project["edited"])
	}
	if hops >= 100 || !objHas(project, "remixParent") || toString(project["remixParent"]) == "" {
		return ""
	}
	parentId := toString(project["remixParent"])
	if parentId == id {
		return ""
	}
	parentResult := loadProject(parentId)
	if parentResult.isErr() {
		return ""
	}
	parentProject := parentResult.data.(Obj)
	if projectPrice(parentProject) > 0 {
		return ""
	}
	return thumbUrlForChain(parentProject, hops+1)
}

func isUntouchedRemix(project Obj) bool {
	if !objHas(project, "remixParent") || toString(project["remixParent"]) == "" {
		return false
	}
	if objHas(project, "inheritedJson") && project["inheritedJson"] == true {
		return true
	}
	return toFloat(project["created"]) == toFloat(project["edited"])
}

func localProjectJsonPathFor(project Obj, hops int) string {
	id := toString(project["id"])
	staged := stagingJsonPath(id)
	if fsExists(staged) {
		return staged
	}
	blobPath := localBlobPath(toString(project["jsonKey"]))
	if fsExists(blobPath) {
		return blobPath
	}
	if hops >= 100 || !isUntouchedRemix(project) {
		return ""
	}
	parentId := toString(project["remixParent"])
	if parentId == id {
		return ""
	}
	parentResult := loadProject(parentId)
	if parentResult.isErr() {
		return ""
	}
	parentProject := parentResult.data.(Obj)
	if projectPrice(parentProject) > 0 {
		return ""
	}
	return localProjectJsonPathFor(parentProject, hops+1)
}

func projectViewKey(project Obj) string {
	if v, ok := project["viewKey"]; ok && v != nil {
		return toString(v)
	}
	return ""
}

func projectJsonBaseUrl(project Obj) string {
	base := publicUrl(toString(project["jsonKey"]))
	if localProjectJsonPathFor(project, 0) != "" || (objHas(project, "pendingJson") && project["pendingJson"] == true) {
		base = appURL + "/api/projects/" + toString(project["id"]) + "/project.json"
	}
	return base
}

func projectJsonUrlFor(project Obj) string {
	base := projectJsonBaseUrl(project)
	if project["shared"] != true {
		k := projectViewKey(project)
		if k != "" {
			return base + "?k=" + k
		}
	}
	return base
}

func blobBase() string { return appURL + "/blobs" }

func r2DirectUrl(key string) string {
	if key == "" || r2PublicBase == "" {
		return ""
	}
	return r2PublicBase + "/" + key
}

func publicUrl(key string) string {
	if key == "" {
		return ""
	}
	return blobBase() + "/" + key
}

func assetsBaseUrl() string { return blobBase() + "/assets" }

func contentTypeForExt(ext string) string {
	types := Obj{
		"json": "application/json", "png": "image/png", "jpg": "image/jpeg",
		"jpeg": "image/jpeg", "gif": "image/gif", "bmp": "image/bmp",
		"svg": "image/svg+xml", "wav": "audio/wav", "mp3": "audio/mpeg",
		"ogg": "audio/ogg", "ttf": "font/ttf", "otf": "font/otf",
		"woff": "font/woff", "woff2": "font/woff2",
	}
	lower := strings.ToLower(ext)
	if v, ok := types[lower]; ok {
		return toString(v)
	}
	return "application/octet-stream"
}

func localBlobPath(key string) string { return localBlobsDir + key }

func isSafeBlobKey(key string) bool {
	if strings.Contains(key, "..") || strings.Contains(key, "\\") || strings.HasPrefix(key, "/") {
		return false
	}
	if strings.HasPrefix(key, "assets/") {
		return isValidMd5Ext(strings.TrimPrefix(key, "assets/"))
	}
	return regexp.MustCompile(`^projects/[A-Za-z0-9]+/project\.json$`).MatchString(key)
}

func contentTypeForKey(key string) string {
	parts := strings.Split(key, ".")
	return contentTypeForExt(parts[len(parts)-1])
}

func parentDirOf(path string) string {
	parts := strings.Split(path, "/")
	dir := ""
	for i, p := range parts {
		if i == len(parts)-1 {
			continue
		}
		if dir == "" {
			dir = p
		} else {
			dir = dir + "/" + p
		}
	}
	return dir
}

// r2PutEncoded / r2Put / r2Remove: write blobs. With r2Local the blob is written
// to local disk. Remote R2 (S3) writes are not yet implemented in this port.
func r2PutEncoded(key string, body []byte, contentType, contentEncoding string) bool {
	if r2Local {
		path := localBlobPath(key)
		if !fsMkdirAll(parentDirOf(path)) {
			return false
		}
		return fsWriteFileBytes(path, body)
	}
	return false
}

func r2Put(key string, body []byte, contentType string) bool {
	return r2PutEncoded(key, body, contentType, "")
}

func r2Remove(key string) bool {
	if r2Local {
		return fsRemove(localBlobPath(key))
	}
	return false
}

func copyStoredProjectJsonToPath(project Obj, outputPath string) bool {
	localPath := localProjectJsonPathFor(project, 0)
	if localPath != "" {
		if projectJsonIsGzip(project) {
			if fsGetSize(localPath) > maxStoredProjectJsonBytes {
				return false
			}
			return copyFile(localPath, outputPath)
		}
		return gzipLimited(localPath, outputPath, maxProjectJsonBytes, maxStoredProjectJsonBytes)
	}
	if r2Local {
		return false
	}
	direct := r2DirectUrl(toString(project["jsonKey"]))
	if direct == "" {
		return false
	}
	fetched := requestsGet(direct, map[string]any{"headers": map[string]any{"Accept-Encoding": "identity"}})
	if !fetched.success || fetched.status != 200 {
		return false
	}
	if projectJsonIsGzip(project) {
		if float64(len(fetched.body)) > maxStoredProjectJsonBytes {
			return false
		}
		return fsWriteFileBytes(outputPath, []byte(fetched.body))
	}
	plainPath := outputPath + ".plain"
	if !fsWriteFileBytes(plainPath, []byte(fetched.body)) {
		return false
	}
	compressed := gzipLimited(plainPath, outputPath, maxProjectJsonBytes, maxStoredProjectJsonBytes)
	fsRemove(plainPath)
	return compressed
}

func blobCacheHeaderEnv(key string) string {
	if strings.HasPrefix(key, "assets/") {
		return "public, max-age=31536000, immutable"
	}
	return "no-cache"
}

func projectJsonEtag(project Obj) string {
	return "\"" + toString(project["id"]) + "-" + toString(project["edited"]) + "\""
}

func serveNotModified(c *Context, project Obj) bool {
	etag := projectJsonEtag(project)
	c.setHeader("ETag", etag)
	c.setHeader("Cache-Control", "no-cache")
	if strings.Contains(c.headerVal("If-None-Match"), etag) {
		c.status(304)
		return true
	}
	return false
}

func serveBlobKey(c *Context, key string) {
	if !isSafeBlobKey(key) {
		c.notFound("blob not found")
		return
	}
	path := localBlobPath(key)
	if fsExists(path) {
		c.setHeader("Content-Type", contentTypeForKey(key))
		c.setHeader("Cache-Control", blobCacheHeaderEnv(key))
		c.file(path)
		return
	}
	if r2Local {
		c.notFound("blob not found")
		return
	}
	direct := r2DirectUrl(key)
	if direct == "" {
		c.notFound("blob not found")
		return
	}
	fetched := requestsGet(direct, nil)
	if !fetched.success || fetched.status != 200 {
		c.notFound("blob not found")
		return
	}
	c.setHeader("Cache-Control", blobCacheHeaderEnv(key))
	c.data(200, contentTypeForKey(key), []byte(fetched.body))
}

func serveProjectJson(c *Context, project Obj) {
	localPath := localProjectJsonPathFor(project, 0)
	if localPath != "" {
		c.setHeader("Content-Type", "application/json")
		c.setHeader("Cache-Control", "no-cache")
		if projectJsonIsGzip(project) {
			c.setHeader("Content-Encoding", "gzip")
		}
		c.file(localPath)
		return
	}
	serveBlobKey(c, toString(project["jsonKey"]))
}

func handleGetAssetBlob(c *Context) {
	serveBlobKey(c, assetObjectKey(c.param("name")))
}

func handleGetProjectBlob(c *Context) {
	id := c.param("id")
	if !isSafePathPart(id) {
		c.notFound("blob not found")
		return
	}
	projectResult := loadProject(id)
	if projectResult.isErr() {
		c.notFound("blob not found")
		return
	}
	project := projectResult.data.(Obj)
	if projectPrice(project) > 0 {
		pk := projectViewKey(project)
		if pk == "" || c.query("k", "") != pk {
			c.notFound("blob not found")
			return
		}
	} else if project["shared"] != true && projectVisibility(project) != "unlisted" {
		k := projectViewKey(project)
		if k == "" || c.query("k", "") != k {
			c.notFound("blob not found")
			return
		}
	}
	if serveNotModified(c, project) {
		return
	}
	serveProjectJson(c, project)
}
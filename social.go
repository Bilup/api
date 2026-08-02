package main

import "strings"

// social.go — maps social.osl.

func ownerIndexEntries(owner string, includeUnshared bool) []any {
	index := loadIndex()
	projects := toObj(index["projects"])
	keys := objKeys(projects)
	ownerKey := normalizeUsername(owner)
	entries := []any{}
	for _, k := range keys {
		entry := toObj(projects[k])
		if normalizeUsername(toString(entry["owner"])) == ownerKey {
			if entry["shared"] == true || includeUnshared {
				entries = append(entries, entry)
			}
		}
	}
	sortObjBy(entries, "edited", "descending")
	return entries
}

func isViewerOwner(c *Context, name string) bool {
	if !c.getBool("authenticated") {
		return false
	}
	if c.getBool("isAdmin") {
		return true
	}
	return normalizeUsername(c.getString("username")) == normalizeUsername(name)
}

func handleGetUser(c *Context) {
	name := c.param("name")
	key := normalizeUsername(name)
	exists := isSafePathPart(key) && fsExists(usersDir+key+".json")
	profile := loadProfile(name)
	followers := asArray(profile["followers"])
	following := asArray(profile["following"])
	viewer := normalizeUsername(c.getString("username"))
	projects := ownerIndexEntries(name, false)
	commentsOff := false
	if profile["commentsOff"] == true {
		commentsOff = true
	}
	c.json(200, Obj{
		"ok":              true,
		"exists":          exists || len(projects) > 0,
		"username":        profile["username"],
		"bio":             profile["bio"],
		"featuredProject": profile["featuredProject"],
		"followerCount":   float64(len(followers)),
		"followingCount":  float64(len(following)),
		"isFollowing":     containsStr(followers, viewer),
		"commentsOff":     commentsOff,
		"projects":        projects,
	})
}

func handleSearchUsers(c *Context) {
	q := normalizeUsername(c.query("q", ""))
	if q == "" {
		c.json(200, Obj{"ok": true, "users": []any{}})
		return
	}
	names := fsReadDir(usersDir)
	sharedCounts := map[string]float64{}
	if index := loadIndex(); index["projects"] != nil {
		for _, k := range objKeys(toObj(index["projects"])) {
			entry := toObj(toObj(index["projects"])[k])
			if entry["shared"] == true {
				owner := normalizeUsername(toString(entry["owner"]))
				sharedCounts[owner]++
			}
		}
	}
	matches := []any{}
	for _, file := range names {
		if strings.HasSuffix(file, ".json") && len(matches) < 8 {
			username := strings.TrimSuffix(file, ".json")
			if strings.Contains(username, q) {
				profile := loadProfile(username)
				projectCount := float64(0)
				if c, ok := sharedCounts[username]; ok {
					projectCount = c
				}
				matches = append(matches, Obj{"username": username, "followers": float64(len(asArray(profile["followers"]))), "projects": projectCount})
			}
		}
	}
	c.json(200, Obj{"ok": true, "users": matches})
}

func handleUpdateProfile(c *Context) {
	username := c.getString("username")
	profile := loadProfile(username)
	body := c.bodyJSON()

	if body["bio"] != nil {
		bio := toString(body["bio"])
		if len(bio) > 300 {
			c.badRequest("bio too long")
			return
		}
		profile["bio"] = bio
	}

	if body["featuredProject"] != nil {
		featured := toString(body["featuredProject"])
		if featured != "" {
			res := loadProject(featured)
			if !res.isOk() {
				c.badRequest("project not found")
				return
			}
			project := toObj(res.unwrap())
			if normalizeUsername(toString(project["owner"])) != normalizeUsername(username) {
				c.forbidden("not your project")
				return
			}
			if project["shared"] != true {
				c.badRequest("share the project before featuring it")
				return
			}
		}
		profile["featuredProject"] = featured
	}

	if body["commentsOff"] != nil {
		profile["commentsOff"] = toBool(body["commentsOff"])
	}

	saveProfile(profile)
	c.json(200, Obj{"ok": true})
}

func handleFollowUser(c *Context) {
	actor := c.getString("username")
	target := c.param("name")
	if normalizeUsername(actor) == normalizeUsername(target) {
		c.badRequest("cannot follow yourself")
		return
	}
	targetProfile := loadProfile(target)
	followers := asArray(targetProfile["followers"])
	wasFollowing := containsStr(followers, normalizeUsername(actor))
	followers = removeStr(followers, normalizeUsername(actor))
	followers = append(followers, normalizeUsername(actor))
	targetProfile["followers"] = followers
	saveProfile(targetProfile)

	actorProfile := loadProfile(actor)
	following := asArray(actorProfile["following"])
	following = removeStr(following, normalizeUsername(target))
	following = append(following, normalizeUsername(target))
	actorProfile["following"] = following
	saveProfile(actorProfile)

	if !wasFollowing {
		addNotification(target, actor, "follow", Obj{})
	}
	c.json(200, Obj{"ok": true})
}

func handleUnfollowUser(c *Context) {
	actor := c.getString("username")
	target := c.param("name")
	targetProfile := loadProfile(target)
	targetProfile["followers"] = removeStr(asArray(targetProfile["followers"]), normalizeUsername(actor))
	saveProfile(targetProfile)
	actorProfile := loadProfile(actor)
	actorProfile["following"] = removeStr(asArray(actorProfile["following"]), normalizeUsername(target))
	saveProfile(actorProfile)
	c.json(200, Obj{"ok": true})
}

func handleGetUserProjects(c *Context) {
	name := c.param("name")
	includeUnshared := isViewerOwner(c, name) && c.queryBool("all", false)
	c.json(200, Obj{"ok": true, "projects": ownerIndexEntries(name, includeUnshared)})
}

func userProjectList(c *Context, field string) {
	profile := loadProfile(c.param("name"))
	ids := asArray(profile[field])
	projects := toObj(loadIndex()["projects"])
	entries := []any{}
	for _, it := range ids {
		id := toString(it)
		if projects[id] != nil {
			entry := toObj(projects[id])
			if entry["shared"] == true {
				entries = append(entries, entry)
			}
		}
	}
	reverseAny(entries)
	c.json(200, Obj{"ok": true, "projects": entries})
}

func handleGetUserLoves(c *Context) { userProjectList(c, "loves") }

func userLibrary(userLower string) []any {
	profile := loadProfile(userLower)
	if profile["library"] != nil {
		return asArray(profile["library"])
	}
	return []any{}
}

func isInLibrary(userLower, projectId string) bool {
	if userLower == "" {
		return false
	}
	return containsStr(userLibrary(userLower), projectId)
}

func addToLibrary(userLower, projectId string) bool {
	profile := loadProfile(userLower)
	lib := []any{}
	if profile["library"] != nil {
		lib = asArray(profile["library"])
	}
	if containsStr(lib, projectId) {
		return false
	}
	lib = append(lib, projectId)
	profile["library"] = lib
	saveProfile(profile)
	return true
}

func bumpProjectSaves(projectId string, delta float64) {
	res := loadProject(projectId)
	if !res.isOk() {
		return
	}
	project := toObj(res.unwrap())
	current := float64(0)
	if project["saves"] != nil {
		current = toFloat(project["saves"])
	}
	next := current + delta
	if next < 0 {
		next = 0
	}
	project["saves"] = next
	saveProject(project)
}

func handleSaveProject(c *Context) {
	res := loadProject(c.param("id"))
	if !res.isOk() {
		c.notFound("project not found")
		return
	}
	project := toObj(res.unwrap())
	if !canViewProject(project, c) {
		c.notFound("project not found")
		return
	}
	added := addToLibrary(normalizeUsername(c.getString("username")), toString(project["id"]))
	if added {
		bumpProjectSaves(toString(project["id"]), 1)
	}
	c.json(200, Obj{"ok": true, "saved": true})
}

func handleUnsaveProject(c *Context) {
	me := normalizeUsername(c.getString("username"))
	profile := loadProfile(me)
	lib := []any{}
	if profile["library"] != nil {
		lib = asArray(profile["library"])
	}
	pid := c.param("id")
	had := containsStr(lib, pid)
	profile["library"] = removeStr(lib, pid)
	saveProfile(profile)
	if had {
		bumpProjectSaves(pid, -1)
	}
	c.json(200, Obj{"ok": true, "saved": false})
}

func addPurchaseRecord(userLower string, entry Obj) {
	profile := loadProfile(userLower)
	list := []any{}
	if profile["purchases"] != nil {
		list = asArray(profile["purchases"])
	}
	next := []any{entry}
	for _, it := range list {
		if len(next) < 200 {
			next = append(next, it)
		}
	}
	profile["purchases"] = next
	saveProfile(profile)
}

func handleGetMyPurchases(c *Context) {
	profile := loadProfile(normalizeUsername(c.getString("username")))
	list := []any{}
	if profile["purchases"] != nil {
		list = asArray(profile["purchases"])
	}
	c.json(200, Obj{"ok": true, "purchases": list})
}

func handleGetMyLibrary(c *Context) {
	ids := userLibrary(normalizeUsername(c.getString("username")))
	projects := toObj(loadIndex()["projects"])
	entries := []any{}
	for _, it := range ids {
		id := toString(it)
		if projects[id] != nil {
			entries = append(entries, projects[id])
		}
	}
	reverseAny(entries)
	c.json(200, Obj{"ok": true, "projects": entries})
}

func handleGetActivity(c *Context) {
	users := strings.Split(c.query("users", ""), ",")
	items := []any{}
	count := 0
	for _, u := range users {
		if count < 50 {
			for _, it := range loadActivity(u) {
				items = append(items, it)
			}
			count++
		}
	}
	sortObjBy(items, "created", "descending")
	if len(items) > 40 {
		items = items[:40]
	}
	c.json(200, Obj{"ok": true, "activity": items})
}

func handleGetUserFollowers(c *Context) {
	profile := loadProfile(c.param("name"))
	c.json(200, Obj{"ok": true, "users": asArray(profile["followers"])})
}

func handleGetUserFollowing(c *Context) {
	profile := loadProfile(c.param("name"))
	c.json(200, Obj{"ok": true, "users": asArray(profile["following"])})
}
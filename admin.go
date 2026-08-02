package main

import (
	"strconv"
	"strings"
)

// admin.osl — identity/ban helpers + admin handlers.

func loadAdmins() []any {
	res := readFile(adminsFile)
	if res.isErr() {
		return []any{}
	}
	data := res.data.(Obj)
	return asArray(data["admins"])
}

func saveAdmins(admins []any) bool {
	adminsCache.delete("admins")
	return writeFile(adminsFile, Obj{"admins": admins})
}

func cachedAdmins() []any {
	if v := adminsCache.get("admins"); v != nil {
		return v.([]any)
	}
	admins := loadAdmins()
	adminsCache.set("admins", admins)
	return admins
}

func loadBans() Obj {
	res := readFile(bansFile)
	if res.isErr() {
		return Obj{"bans": Obj{}}
	}
	return res.data.(Obj)
}

func saveBans(data Obj) bool {
	bansCache.delete("bans")
	return writeFile(bansFile, data)
}

func cachedBans() Obj {
	if v := bansCache.get("bans"); v != nil {
		return v.(Obj)
	}
	data := loadBans()
	bansCache.set("bans", data)
	return data
}

func isBannedUser(username string) bool {
	key := normalizeUsername(username)
	if key == "" {
		return false
	}
	return objHas(cachedBans()["bans"].(map[string]any), key)
}

func banReasonFor(username string) string {
	key := normalizeUsername(username)
	if key == "" {
		return ""
	}
	bans := cachedBans()["bans"].(map[string]any)
	raw, ok := bans[key]
	if !ok {
		return ""
	}
	entry := raw.(map[string]any)
	if v, ok := entry["reason"]; ok && v != nil {
		return toString(v)
	}
	return ""
}

func banUser(username, reason, actor string) bool {
	key := normalizeUsername(username)
	if !isSafePathPart(key) {
		return false
	}
	data := loadBans()
	bans := data["bans"].(map[string]any)
	bans[key] = Obj{"reason": reason, "by": actor, "created": nowMs()}
	return saveBans(data)
}

func unbanUser(username string) bool {
	key := normalizeUsername(username)
	data := loadBans()
	bans := data["bans"].(map[string]any)
	if !objHas(bans, key) {
		return false
	}
	delete(bans, key)
	return saveBans(data)
}

func isSuperAdmin(username string) bool {
	for _, a := range adminUsers {
		if a == normalizeUsername(username) {
			return true
		}
	}
	return false
}

func handleListAdmins(c *Context) {
	stored := loadAdmins()
	all := []any{}
	for _, name := range adminUsers {
		all = append(all, Obj{"username": name, "super": true})
	}
	for _, it := range stored {
		name := toString(it)
		if !strContains(adminUsers, normalizeUsername(name)) {
			all = append(all, Obj{"username": name, "super": false})
		}
	}
	c.json(200, Obj{"ok": true, "admins": all})
}

func handleAddAdmin(c *Context) {
	if !isSuperAdmin(c.getString("username")) {
		c.forbidden("only super admins can manage admins")
		return
	}
	body := c.bodyJSON()
	name := normalizeUsername(toString(body["username"]))
	if !isSafePathPart(name) {
		c.badRequest("invalid username")
		return
	}
	admins := loadAdmins()
	if containsStr(admins, name) {
		c.json(200, Obj{"ok": true})
		return
	}
	admins = append(admins, name)
	saveAdmins(admins)
	c.json(200, Obj{"ok": true})
}

func handleRemoveAdmin(c *Context) {
	if !isSuperAdmin(c.getString("username")) {
		c.forbidden("only super admins can manage admins")
		return
	}
	body := c.bodyJSON()
	name := normalizeUsername(toString(body["username"]))
	admins := loadAdmins()
	if !containsStr(admins, name) {
		c.notFound("not an admin")
		return
	}
	saveAdmins(removeStr(admins, name))
	c.json(200, Obj{"ok": true})
}

func handleBanUser(c *Context) {
	body := c.bodyJSON()
	name := toString(body["username"])
	reason := ""
	if body["reason"] != nil {
		reason = strings.TrimSpace(toString(body["reason"]))
	}
	if normalizeUsername(name) == "" {
		c.badRequest("username is required")
		return
	}
	if isAdminUser(name) {
		c.badRequest("cannot ban an admin")
		return
	}
	if !banUser(name, reason, c.getString("username")) {
		c.badRequest("could not ban that user")
		return
	}
	c.json(200, Obj{"ok": true})
}

func handleUnbanUser(c *Context) {
	body := c.bodyJSON()
	if !unbanUser(toString(body["username"])) {
		c.notFound("that user is not banned")
		return
	}
	c.json(200, Obj{"ok": true})
}

func handleListBans(c *Context) {
	bans := toObj(loadBans()["bans"])
	out := []any{}
	for key, v := range bans {
		ban := toObj(v)
		out = append(out, Obj{"username": key, "reason": ban["reason"], "by": ban["by"], "created": ban["created"]})
	}
	c.json(200, Obj{"ok": true, "bans": out})
}

func handleListUsers(c *Context) {
	userFiles := fsReadDir(usersDir)
	projects := toObj(loadIndex()["projects"])
	ownerCounts := Obj{}
	for _, pk := range objKeys(projects) {
		entry := toObj(projects[pk])
		owner := normalizeUsername(toString(entry["owner"]))
		prev := float64(0)
		if ownerCounts[owner] != nil {
			prev = toFloat(ownerCounts[owner])
		}
		ownerCounts[owner] = prev + 1
	}
	out := []any{}
	for _, f := range userFiles {
		if strings.HasSuffix(f, ".json") {
			key := strings.TrimSuffix(f, ".json")
			profile := loadProfile(key)
			quota := quotaDetails(key)
			projectCount := float64(0)
			if ownerCounts[key] != nil {
				projectCount = toFloat(ownerCounts[key])
			}
			out = append(out, Obj{
				"username":            profile["username"],
				"created":             profile["created"],
				"followerCount":       float64(len(asArray(profile["followers"]))),
				"projectCount":        projectCount,
				"quotaUsed":           quota["used"],
				"quotaLimit":          quota["limit"],
				"quotaOldestEventMs":  quota["oldestEventMs"],
				"quotaEventCount":     quota["eventCount"],
				"banned":              isBannedUser(key),
			})
		}
	}
	sortObjBy(out, "quotaUsed", "descending")
	c.json(200, Obj{"ok": true, "users": out})
}

func handleAdminStats(c *Context) {
	projects := toObj(loadIndex()["projects"])
	keys := objKeys(projects)
	totalProjects := float64(len(keys))
	sharedProjects := float64(0)
	totalBytes := float64(0)
	totalViews := float64(0)
	totalLoves := float64(0)
	projectsByDay := Obj{}
	for _, key := range keys {
		entry := toObj(projects[key])
		if entry["shared"] == true {
			sharedProjects++
		}
		if entry["sizeBytes"] != nil {
			totalBytes += toFloat(entry["sizeBytes"])
		}
		if entry["views"] != nil {
			totalViews += toFloat(entry["views"])
		}
		if entry["loveCount"] != nil {
			totalLoves += toFloat(entry["loveCount"])
		}
		dk := strconv.Itoa(int(toFloat(entry["created"]) / 86400000))
		prev := float64(0)
		if projectsByDay[dk] != nil {
			prev = toFloat(projectsByDay[dk])
		}
		projectsByDay[dk] = prev + 1
	}

	totalUsers := float64(0)
	for _, f := range fsReadDir(usersDir) {
		if strings.HasSuffix(f, ".json") {
			totalUsers++
		}
	}

	loginsByDay := Obj{}
	activeSessions := float64(0)
	for _, sf := range fsReadDir(sessionsDir) {
		if !strings.HasSuffix(sf, ".json") {
			continue
		}
		res := readFile(sessionsDir + sf)
		if !res.isOk() {
			continue
		}
		s := toObj(res.unwrap())
		activeSessions++
		dk := strconv.Itoa(int(toFloat(s["createdAt"]) / 86400000))
		prev := float64(0)
		if loginsByDay[dk] != nil {
			prev = toFloat(loginsByDay[dk])
		}
		loginsByDay[dk] = prev + 1
	}

	openReports := float64(0)
	for _, it := range loadReports() {
		r := toObj(it)
		if r["resolved"] != true {
			openReports++
		}
	}

	bans := toObj(loadBans()["bans"])
	bannedUsers := float64(len(bans))
	news := loadNews()
	pendingPayouts := asArray(toObj(loadPendingPayouts())["payouts"])
	pendingPayoutAmount := float64(0)
	for _, it := range pendingPayouts {
		pendingPayoutAmount += toFloat(toObj(it)["amount"])
	}

	c.json(200, Obj{
		"ok":                  true,
		"totalProjects":       totalProjects,
		"sharedProjects":      sharedProjects,
		"unsharedProjects":    totalProjects - sharedProjects,
		"totalUsers":          totalUsers,
		"totalBytes":          totalBytes,
		"totalViews":          totalViews,
		"totalLoves":          totalLoves,
		"activeSessions":      activeSessions,
		"openReports":         openReports,
		"bannedUsers":         bannedUsers,
		"newsPosts":           float64(len(news)),
		"pendingPayouts":      float64(len(pendingPayouts)),
		"pendingPayoutAmount": pendingPayoutAmount,
		"projectsByDay":       projectsByDay,
		"loginsByDay":         loginsByDay,
	})
}

func handleAdminSearchProjects(c *Context) {
	q := strings.ToLower(strings.TrimSpace(c.query("q", "")))
	projects := toObj(loadIndex()["projects"])
	keys := objKeys(projects)
	matches := []any{}
	for _, key := range keys {
		entry := toObj(projects[key])
		hay := strings.ToLower(toString(entry["title"]) + " " + toString(entry["owner"]) + " " + toString(entry["id"]))
		if q == "" || strings.Contains(hay, q) {
			matches = append(matches, entry)
		}
	}
	sortObjBy(matches, "edited", "descending")
	if len(matches) > 60 {
		matches = matches[:60]
	}
	c.json(200, Obj{"ok": true, "projects": matches})
}

func reportTargetUser(found Obj) string {
	name := toString(found["target"])
	if toString(found["type"]) == "project" {
		result := loadProject(toString(found["target"]))
		if result.isOk() {
			name = toString(toObj(result.unwrap())["owner"])
		}
	}
	return name
}

func handleReportAction(c *Context) {
	body := c.bodyJSON()
	id := toString(body["id"])
	action := toString(body["action"])
	valid := action == "dismiss" || action == "unshare_project" || action == "ban_user" || action == "warn_user"
	if !valid {
		c.badRequest("invalid action")
		return
	}

	reports := loadReports()
	var found Obj
	hasReport := false
	for _, it := range reports {
		report := toObj(it)
		if toString(report["id"]) == id {
			found = report
			hasReport = true
		}
	}
	if !hasReport {
		c.notFound("report not found")
		return
	}

	if action == "unshare_project" {
		result := loadProject(toString(found["target"]))
		if !result.isOk() {
			c.notFound("project not found")
			return
		}
		project := toObj(result.unwrap())
		project["shared"] = false
		project["viewKey"] = randomString(16)
		project["edited"] = timestamp()
		saveProject(project)
		upsertIndexEntry(project)
	}
	if action == "ban_user" {
		name := reportTargetUser(found)
		if isAdminUser(name) {
			c.badRequest("cannot ban an admin")
			return
		}
		banUser(name, "report "+id, c.getString("username"))
	}
	if action == "warn_user" {
		name := reportTargetUser(found)
		if isAdminUser(name) {
			c.badRequest("cannot warn an admin")
			return
		}
		reason := ""
		if body["reason"] != nil {
			reason = strings.TrimSpace(toString(body["reason"]))
		}
		if reason == "" {
			reason = "report " + id
		}
		setStanding(name, "warning", reason, c.getString("username"))
		if toString(found["type"]) == "project" {
			pr := loadProject(toString(found["target"]))
			if pr.isOk() {
				purgeProject(toObj(pr.unwrap()))
			}
		}
	}

	found["resolved"] = true
	found["resolvedBy"] = c.getString("username")
	found["action"] = action
	found["resolvedAt"] = timestamp()
	saveReports(reports)
	if toString(found["type"]) == "project" {
		maybeClearProjectEvidence(toString(found["target"]))
	}
	addNotification(toString(found["reporter"]), c.getString("username"), "report_update", Obj{"action": action, "reportType": found["type"]})
	c.json(200, Obj{"ok": true})
}
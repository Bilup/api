package main

// user-identity.osl

func loadUserIds() Obj {
	res := readFile(userIdsFile)
	if res.isErr() {
		return Obj{"users": Obj{}}
	}
	return res.data.(Obj)
}

func saveUserIds(data Obj) bool { return writeFile(userIdsFile, data) }

func replaceUserInArray(items []any, oldKey, newKey string) []any {
	next := []any{}
	for _, v := range items {
		if normalizeUsername(toString(v)) == oldKey {
			next = append(next, newKey)
		} else {
			next = append(next, v)
		}
	}
	return next
}

func renameUserFile(dir, oldKey, newKey string) {
	if oldKey != newKey && fsExists(dir+oldKey+".json") {
		fsRename(dir+oldKey+".json", dir+newKey+".json")
	}
}

func renameUserInProfiles(oldKey, newKey, newName string) {
	renameUserFile(usersDir, oldKey, newKey)
	profile := loadProfile(newName)
	profile["username"] = newName
	saveProfile(profile)
	for _, name := range fsReadDir(usersDir) {
		if !stringsSuffix(name, ".json") || name == newKey+".json" {
			continue
		}
		res := readFile(usersDir + name)
		if res.isErr() {
			continue
		}
		other := res.data.(Obj)
		changed := false
		followers := asArray(other["followers"])
		for _, f := range followers {
			if normalizeUsername(toString(f)) == oldKey {
				other["followers"] = replaceUserInArray(followers, oldKey, newKey)
				changed = true
				break
			}
		}
		following := asArray(other["following"])
		for _, f := range following {
			if normalizeUsername(toString(f)) == oldKey {
				other["following"] = replaceUserInArray(following, oldKey, newKey)
				changed = true
				break
			}
		}
		if changed {
			writeFile(usersDir+name, other)
		}
	}
}

func renameUserInProjects(old, newKey, newName string) {
	for _, name := range fsReadDir(projectsDir) {
		if !stringsSuffix(name, ".json") {
			continue
		}
		res := loadProject(stringsTrimSuffix(name, ".json"))
		if res.isErr() {
			continue
		}
		project := res.data.(Obj)
		changed := false
		if normalizeUsername(toString(project["owner"])) == old {
			project["owner"] = newName
			changed = true
		}
		loves := asArray(project["loves"])
		for _, l := range loves {
			if normalizeUsername(toString(l)) == old {
				project["loves"] = replaceUserInArray(loves, old, newKey)
				changed = true
				break
			}
		}
		if objHas(project, "brokenhearts") && project["brokenhearts"] != nil {
			broken := asArray(project["brokenhearts"])
			for _, b := range broken {
				if normalizeUsername(toString(b)) == old {
					project["brokenhearts"] = replaceUserInArray(broken, old, newKey)
					changed = true
					break
				}
			}
		}
		if changed {
			saveProject(project)
			upsertIndexEntry(project)
		}
	}
}

func renameUserInComments(old, newKey, newName string) {
	if old != newKey {
		renameUserFile(commentsDir, "profile-"+old, "profile-"+newKey)
		if fsExists(commentsDir + "profile-" + old + ".jsonl") {
			fsRename(commentsDir+"profile-"+old+".jsonl", commentsDir+"profile-"+newKey+".jsonl")
		}
	}
	for _, name := range fsReadDir(commentsDir) {
		key := stringsTrimSuffix(stringsTrimSuffix(name, ".jsonl"), ".json")
		comments := loadComments(key)
		changed := false
		for _, c := range comments {
			comment := c.(map[string]any)
			if normalizeUsername(toString(comment["author"])) == old {
				comment["author"] = newName
				changed = true
			}
		}
		if changed {
			saveComments(key, comments)
		}
	}
}

func renameUserInNotifications(old, newKey, newName string) {
	renameUserFile(notificationsDir, old, newKey)
	for _, name := range fsReadDir(notificationsDir) {
		if !stringsSuffix(name, ".json") {
			continue
		}
		res := readFile(notificationsDir + name)
		if res.isErr() {
			continue
		}
		data := res.data.(Obj)
		items := asArray(data["items"])
		changed := false
		for _, itemRaw := range items {
			item := itemRaw.(map[string]any)
			if objHas(item, "actor") && item["actor"] != nil && normalizeUsername(toString(item["actor"])) == old {
				item["actor"] = newName
				changed = true
			}
		}
		if changed {
			writeFile(notificationsDir+name, data)
		}
	}
}

func renameUserInActivity(old, newKey string) {
	renameUserFile(activityDir, old, newKey)
	res := readFile(activityDir + newKey + ".json")
	if res.isErr() {
		return
	}
	data := res.data.(Obj)
	items := asArray(data["items"])
	for _, itemRaw := range items {
		item := itemRaw.(map[string]any)
		if normalizeUsername(toString(item["actor"])) == old {
			item["actor"] = newKey
		}
	}
	writeFile(activityDir+newKey+".json", data)
}

func renameUser(oldName, newName string) {
	oldKey := normalizeUsername(oldName)
	newKey := normalizeUsername(newName)
	if !isSafePathPart(oldKey) || !isSafePathPart(newKey) {
		return
	}
	renameUserInProfiles(oldKey, newKey, newName)
	renameUserInProjects(oldKey, newKey, newName)
	renameUserInComments(oldKey, newKey, newName)
	renameUserInNotifications(oldKey, newKey, newName)
	renameUserInActivity(oldKey, newKey)
	renameUserFile(userSettingsDir, oldKey, newKey)
	renameUserFile(quotaDir, oldKey, newKey)
	admins := loadAdmins()
	for _, a := range admins {
		if normalizeUsername(toString(a)) == oldKey {
			saveAdmins(replaceUserInArray(admins, oldKey, newKey))
			break
		}
	}
	banData := loadBans()
	bans := banData["bans"].(map[string]any)
	if objHas(bans, oldKey) {
		bans[newKey] = bans[oldKey]
		delete(bans, oldKey)
		saveBans(banData)
	}
}

func ensureUserIdentity(userId, username string) {
	if userId == "" || username == "" {
		return
	}
	data := loadUserIds()
	users := data["users"].(map[string]any)
	current := ""
	if objHas(users, userId) {
		current = toString(users[userId])
	}
	if current != "" && current != username {
		renameUser(current, username)
	}
	if current != username {
		users[userId] = username
		saveUserIds(data)
		userIdsCache.set(userId, username)
	}
}

func usernameForId(userId string) string {
	if v := userIdsCache.get(userId); v != nil {
		return toString(v)
	}
	data := loadUserIds()
	users := data["users"].(map[string]any)
	if objHas(users, userId) {
		userIdsCache.set(userId, users[userId])
		return toString(users[userId])
	}
	return ""
}

func removeUserFromArray(items []any, key string) []any {
	next := []any{}
	for _, v := range items {
		if normalizeUsername(toString(v)) != key {
			next = append(next, v)
		}
	}
	return next
}

func purgeUserFromProfiles(key string) {
	fsRemove(usersDir + key + ".json")
	for _, name := range fsReadDir(usersDir) {
		if !stringsSuffix(name, ".json") {
			continue
		}
		res := readFile(usersDir + name)
		if res.isErr() {
			continue
		}
		other := res.data.(Obj)
		changed := false
		followers := asArray(other["followers"])
		for _, f := range followers {
			if normalizeUsername(toString(f)) == key {
				other["followers"] = removeUserFromArray(followers, key)
				changed = true
				break
			}
		}
		following := asArray(other["following"])
		for _, f := range following {
			if normalizeUsername(toString(f)) == key {
				other["following"] = removeUserFromArray(following, key)
				changed = true
				break
			}
		}
		if changed {
			writeFile(usersDir+name, other)
		}
	}
}

func purgeUserFromProjects(key string) {
	for _, name := range fsReadDir(projectsDir) {
		if !stringsSuffix(name, ".json") {
			continue
		}
		res := loadProject(stringsTrimSuffix(name, ".json"))
		if res.isErr() {
			continue
		}
		project := res.data.(Obj)
		changed := false
		if normalizeUsername(toString(project["owner"])) == key {
			project["owner"] = "Deleted User"
			changed = true
		}
		loves := asArray(project["loves"])
		for _, l := range loves {
			if normalizeUsername(toString(l)) == key {
				project["loves"] = removeUserFromArray(loves, key)
				changed = true
				break
			}
		}
		if objHas(project, "brokenhearts") && project["brokenhearts"] != nil {
			broken := asArray(project["brokenhearts"])
			for _, b := range broken {
				if normalizeUsername(toString(b)) == key {
					project["brokenhearts"] = removeUserFromArray(broken, key)
					changed = true
					break
				}
			}
		}
		if changed {
			saveProject(project)
			upsertIndexEntry(project)
		}
	}
}

func purgeUserFromComments(key string) {
	fsRemove(commentsDir + "profile-" + key + ".jsonl")
	fsRemove(commentsDir + "profile-" + key + ".json")
	for _, name := range fsReadDir(commentsDir) {
		ckey := stringsTrimSuffix(stringsTrimSuffix(name, ".jsonl"), ".json")
		comments := loadComments(ckey)
		changed := false
		for _, c := range comments {
			comment := c.(map[string]any)
			if normalizeUsername(toString(comment["author"])) == key {
				comment["author"] = "Deleted User"
				changed = true
			}
		}
		if changed {
			saveComments(ckey, comments)
		}
	}
}

func purgeUserFromNotifications(key string) {
	fsRemove(notificationsDir + key + ".json")
	for _, name := range fsReadDir(notificationsDir) {
		if !stringsSuffix(name, ".json") {
			continue
		}
		res := readFile(notificationsDir + name)
		if res.isErr() {
			continue
		}
		data := res.data.(Obj)
		items := asArray(data["items"])
		kept := []any{}
		changed := false
		for _, itemRaw := range items {
			item := itemRaw.(map[string]any)
			if objHas(item, "actor") && item["actor"] != nil && normalizeUsername(toString(item["actor"])) == key {
				changed = true
			} else {
				kept = append(kept, itemRaw)
			}
		}
		if changed {
			data["items"] = kept
			writeFile(notificationsDir+name, data)
		}
	}
}

func purgeUserSessions(userId string) {
	for _, name := range fsReadDir(sessionsDir) {
		if !stringsSuffix(name, ".json") {
			continue
		}
		res := readFile(sessionsDir + name)
		if res.isErr() {
			continue
		}
		session := res.data.(Obj)
		if toString(session["userId"]) == userId {
			fsRemove(sessionsDir + name)
		}
	}
}

func purgeUser(userId, username string) {
	key := normalizeUsername(username)
	if !isSafePathPart(key) {
		return
	}
	purgeUserFromProfiles(key)
	purgeUserFromProjects(key)
	purgeUserFromComments(key)
	purgeUserFromNotifications(key)
	fsRemove(activityDir + key + ".json")
	fsRemove(userSettingsDir + key + ".json")
	fsRemove(quotaDir + key + ".json")
	fsRemove(agreementAcceptDir + key + ".json")
	admins := loadAdmins()
	for _, a := range admins {
		if normalizeUsername(toString(a)) == key {
			saveAdmins(removeUserFromArray(admins, key))
			break
		}
	}
	banData := loadBans()
	bans := banData["bans"].(map[string]any)
	if objHas(bans, key) {
		delete(bans, key)
		saveBans(banData)
	}
	purgeUserSessions(userId)
	idData := loadUserIds()
	idUsers := idData["users"].(map[string]any)
	if objHas(idUsers, userId) {
		delete(idUsers, userId)
		saveUserIds(idData)
	}
	userIdsCache.set(userId, "")
}

func reconcileDeletedUsers() {
	idData := loadUserIds()
	idUsers := idData["users"].(map[string]any)
	ids := objKeys(idUsers)
	if len(ids) == 0 {
		return
	}
	resp := requestsPost(roturBase+"/accounts/deleted_check", map[string]any{
		"headers": map[string]any{"Content-Type": "application/json"},
		"body":    jsonString(Obj{"ids": ids}),
	})
	if !resp.success {
		fmtLog("reconcileDeletedUsers: rotur request failed")
		return
	}
	parsed := tryParseJSON(resp.body, "object")
	if parsed.isErr() {
		fmtLog("reconcileDeletedUsers: invalid response from rotur")
		return
	}
	data := parsed.data.(Obj)
	if !objHas(data, "deleted") || data["deleted"] == nil {
		return
	}
	for _, uid := range asArray(data["deleted"]) {
		u := toString(uid)
		if objHas(idUsers, u) {
			uname := toString(idUsers[u])
			if uname != "" {
				fmtLog("Purging deleted rotur account " + u + " (" + uname + ")")
				purgeUser(u, uname)
			}
		}
	}
}

func reconcileLoop() {
	for {
		reconcileDeletedUsers()
		sleepMs(300000)
	}
}

func handleReconcileDeletions(c *Context) {
	reconcileDeletedUsers()
	c.ok(Obj{"ok": true})
}

func stringsTrimSuffix(s, suffix string) string {
	if len(s) >= len(suffix) && s[len(s)-len(suffix):] == suffix {
		return s[:len(s)-len(suffix)]
	}
	return s
}
func stringsSuffix(s, suffix string) bool {
	return len(s) >= len(suffix) && s[len(s)-len(suffix):] == suffix
}
package main

import (
	"strings"
)

// community-tools.go — maps community-tools.osl + database.osl.
// File-based document collections for Spaces, Releases, Previews,
// Contributions, Diagnostics, Feedback, Reviews, Ideas/Roadmap, Project Card.

// ---- document collection helpers (file-based) ----

func loadCollection(path string) []any {
	res := readFile(path)
	if res.isErr() {
		return []any{}
	}
	data := toObj(res.unwrap())
	return asArray(data["items"])
}

func saveCollection(path string, items []any) bool {
	return writeFile(path, Obj{"items": items})
}

func collectionFindById(path, id string) (Obj, []any, int) {
	items := loadCollection(path)
	for i, it := range items {
		doc := toObj(it)
		if toString(doc["_id"]) == id {
			return doc, items, i
		}
	}
	return Obj{}, items, -1
}

func collectionInsertOne(path string, doc Obj) bool {
	items := loadCollection(path)
	items = append(items, doc)
	return saveCollection(path, items)
}

func collectionDeleteOne(path, id string) bool {
	_, items, idx := collectionFindById(path, id)
	if idx < 0 {
		return false
	}
	items = append(items[:idx], items[idx+1:]...)
	return saveCollection(path, items)
}

func collectionReplaceOne(path string, doc Obj) bool {
	id := toString(doc["_id"])
	_, items, idx := collectionFindById(path, id)
	if idx < 0 {
		return false
	}
	items[idx] = doc
	return saveCollection(path, items)
}

func collectionWhere(path, field, op, value string) []any {
	items := loadCollection(path)
	out := []any{}
	for _, it := range items {
		doc := toObj(it)
		match := false
		switch op {
		case "equal":
			match = toString(doc[field]) == value
		default:
			match = toString(doc[field]) == value
		}
		if match {
			out = append(out, doc)
		}
	}
	return out
}

func collectionSort(items []any, key, dir string) {
	sortObjBy(items, key, dir)
}

func collectionLimit(items []any, n int) []any {
	if len(items) > n {
		return items[:n]
	}
	return items
}

// putDocument mirrors database.osl putDocument: upsert by _id.
func putDocument(collectionPath, id string, value Obj) bool {
	value["_id"] = id
	_, items, idx := collectionFindById(collectionPath, id)
	if idx >= 0 {
		items[idx] = value
	} else {
		items = append(items, value)
	}
	return saveCollection(collectionPath, items)
}

// ---- string helpers ----

func stringArrayContainsUser(users []any, username string) bool {
	wanted := normalizeUsername(username)
	for _, u := range users {
		if normalizeUsername(toString(u)) == wanted {
			return true
		}
	}
	return false
}

func validSpaceKind(kind string) bool {
	return kind == "studio" || kind == "collection" || kind == "challenge"
}

func validSpaceVisibility(visibility string) bool {
	return visibility == "public" || visibility == "unlisted" || visibility == "private"
}

func spaceManagers(space Obj) []any {
	if space["managers"] != nil {
		return asArray(space["managers"])
	}
	return []any{}
}

func spaceCuratorInvites(space Obj) []any {
	if space["curatorInvites"] != nil {
		return asArray(space["curatorInvites"])
	}
	return []any{}
}

func spaceLikes(space Obj) []any {
	if space["likes"] != nil {
		return asArray(space["likes"])
	}
	return []any{}
}

func spaceBrokenHearts(space Obj) []any {
	if space["brokenHearts"] != nil {
		return asArray(space["brokenHearts"])
	}
	return []any{}
}

func spaceCommentKey(id string) string {
	return "space-" + id
}

func isSpaceOwner(space Obj, c *Context) bool {
	return c.getBool("isAdmin") || normalizeUsername(toString(space["owner"])) == normalizeUsername(c.getString("username"))
}

func isSpaceInvitee(space Obj, c *Context) bool {
	invites := spaceCuratorInvites(space)
	username := normalizeUsername(c.getString("username"))
	for _, it := range invites {
		invite := toObj(it)
		if normalizeUsername(toString(invite["username"])) == username {
			return true
		}
	}
	return false
}

func canManageSpace(space Obj, c *Context) bool {
	if c.getBool("isAdmin") {
		return true
	}
	username := c.getString("username")
	if normalizeUsername(toString(space["owner"])) == normalizeUsername(username) {
		return true
	}
	return stringArrayContainsUser(spaceManagers(space), username)
}

func canViewSpace(space Obj, c *Context) bool {
	vis := toString(space["visibility"])
	if vis == "public" || vis == "unlisted" {
		return true
	}
	return canManageSpace(space, c) || isSpaceInvitee(space, c)
}

func spaceResponse(space Obj, c *Context) Obj {
	projects := []any{}
	ids := asArray(space["projects"])
	for _, it := range ids {
		loaded := loadProject(toString(it))
		if loaded.isOk() {
			project := toObj(loaded.unwrap())
			if project["shared"] == true || projectVisibility(project) == "unlisted" || canManageSpace(space, c) {
				projects = append(projects, indexEntryFromProject(project))
			}
		}
	}

	response := Obj{}
	for k, v := range space {
		response[k] = v
	}
	response["projects"] = projects
	response["projectIds"] = ids
	response["canManage"] = canManageSpace(space, c)
	response["isOwner"] = isSpaceOwner(space, c)
	response["following"] = stringArrayContainsUser(asArray(space["followers"]), c.getString("username"))
	response["invited"] = isSpaceInvitee(space, c)

	likes := spaceLikes(space)
	brokenHearts := spaceBrokenHearts(space)
	response["likeCount"] = float64(len(likes))
	response["brokenHeartCount"] = float64(len(brokenHearts))
	response["commentCount"] = float64(len(loadComments(spaceCommentKey(toString(space["_id"])))))

	myReaction := ""
	if stringArrayContainsUser(likes, c.getString("username")) {
		myReaction = "heart"
	} else if stringArrayContainsUser(brokenHearts, c.getString("username")) {
		myReaction = "brokenheart"
	}
	response["myReaction"] = myReaction
	response["likes"] = []any{}
	response["brokenHearts"] = []any{}
	response["curatorInvites"] = []any{}
	return response
}

func spaceManagementResponse(space Obj, c *Context) Obj {
	invites := spaceCuratorInvites(space)
	response := spaceResponse(space, c)
	response["curatorInvites"] = invites
	return response
}

// ---- Space handlers ----

func handleListSpaces(c *Context) {
	kind := strings.ToLower(strings.TrimSpace(c.query("kind", "")))
	q := strings.ToLower(strings.TrimSpace(c.query("q", "")))
	stored := loadCollection(spacesFile)
	collectionSort(stored, "edited", "descending")
	spaces := []any{}
	for _, it := range stored {
		space := toObj(it)
		include := toString(space["visibility"]) == "public"
		if include && kind != "" {
			include = toString(space["kind"]) == kind
		}
		if include && q != "" {
			title := strings.ToLower(toString(space["title"]))
			desc := strings.ToLower(toString(space["description"]))
			include = strings.Contains(title, q) || strings.Contains(desc, q)
		}
		if include {
			spaces = append(spaces, spaceResponse(space, c))
		}
	}
	c.json(200, Obj{"ok": true, "spaces": spaces})
}

func handleGetSpace(c *Context) {
	space, _, _ := collectionFindById(spacesFile, c.param("id"))
	if len(space) == 0 || !canViewSpace(space, c) {
		c.notFound("space not found")
		return
	}
	c.json(200, Obj{"ok": true, "space": spaceResponse(space, c)})
}

func handleCreateSpace(c *Context) {
	body := c.bodyJSON()
	title := strings.TrimSpace(toString(body["title"]))
	kind := strings.ToLower(strings.TrimSpace(toString(body["kind"])))
	if title == "" || len(title) > 100 {
		c.badRequest("title must be between 1 and 100 characters")
		return
	}
	if !validSpaceKind(kind) {
		c.badRequest("invalid space type")
		return
	}
	visibility := "public"
	if body["visibility"] != nil {
		visibility = strings.ToLower(toString(body["visibility"]))
	}
	if !validSpaceVisibility(visibility) {
		c.badRequest("invalid visibility")
		return
	}
	description := ""
	if body["description"] != nil {
		description = strings.TrimSpace(toString(body["description"]))
	}
	if len(description) > 5000 {
		c.badRequest("description is too long")
		return
	}
	startsAt := float64(0)
	if body["startsAt"] != nil {
		startsAt = toFloat(body["startsAt"])
	}
	endsAt := float64(0)
	if body["endsAt"] != nil {
		endsAt = toFloat(body["endsAt"])
	}
	id := "s" + randomString(12)
	space := Obj{
		"_id":             id,
		"title":           title,
		"description":     description,
		"kind":            kind,
		"visibility":      visibility,
		"owner":           c.getString("username"),
		"managers":        []any{},
		"curatorInvites":  []any{},
		"projects":        []any{},
		"followers":       []any{},
		"likes":           []any{},
		"brokenHearts":    []any{},
		"openSubmissions": body["openSubmissions"] != nil && toBool(body["openSubmissions"]),
		"startsAt":        startsAt,
		"endsAt":          endsAt,
		"created":         float64(timestamp()),
		"edited":          float64(timestamp()),
	}
	collectionInsertOne(spacesFile, space)
	c.json(201, Obj{"ok": true, "space": spaceResponse(space, c)})
}

func handleUpdateSpace(c *Context) {
	space, _, _ := collectionFindById(spacesFile, c.param("id"))
	if len(space) == 0 {
		c.notFound("space not found")
		return
	}
	if !canManageSpace(space, c) {
		c.forbidden("you cannot manage this space")
		return
	}
	body := c.bodyJSON()
	if body["title"] != nil {
		title := strings.TrimSpace(toString(body["title"]))
		if title == "" || len(title) > 100 {
			c.badRequest("invalid title")
			return
		}
		space["title"] = title
	}
	if body["description"] != nil {
		description := strings.TrimSpace(toString(body["description"]))
		if len(description) > 5000 {
			c.badRequest("description is too long")
			return
		}
		space["description"] = description
	}
	if body["visibility"] != nil && validSpaceVisibility(toString(body["visibility"])) {
		space["visibility"] = body["visibility"]
	}
	if body["openSubmissions"] != nil {
		space["openSubmissions"] = toBool(body["openSubmissions"])
	}
	if body["startsAt"] != nil {
		space["startsAt"] = toFloat(body["startsAt"])
	}
	if body["endsAt"] != nil {
		space["endsAt"] = toFloat(body["endsAt"])
	}
	space["edited"] = float64(timestamp())
	putDocument(spacesFile, toString(space["_id"]), space)
	c.json(200, Obj{"ok": true, "space": spaceResponse(space, c)})
}

func handleDeleteSpace(c *Context) {
	space, _, _ := collectionFindById(spacesFile, c.param("id"))
	if len(space) == 0 {
		c.notFound("space not found")
		return
	}
	if normalizeUsername(toString(space["owner"])) != normalizeUsername(c.getString("username")) && !c.getBool("isAdmin") {
		c.forbidden("only the owner can delete this space")
		return
	}
	collectionDeleteOne(spacesFile, toString(space["_id"]))
	c.json(200, Obj{"ok": true})
}

func handleAddSpaceProject(c *Context) {
	space, _, _ := collectionFindById(spacesFile, c.param("id"))
	projectRes := loadProject(c.param("project"))
	if len(space) == 0 || projectRes.isErr() {
		c.notFound("space or project not found")
		return
	}
	project := toObj(projectRes.unwrap())
	projectVisible := project["shared"] == true || projectVisibility(project) == "unlisted"
	if !projectVisible {
		c.forbidden("only shared or unlisted projects can be added")
		return
	}
	allowed := canManageSpace(space, c)
	ownsProject := normalizeUsername(toString(project["owner"])) == normalizeUsername(c.getString("username"))
	allowed = allowed || (toBool(space["openSubmissions"]) && ownsProject)
	if !allowed {
		c.forbidden("submissions are closed")
		return
	}
	ids := asArray(space["projects"])
	if !containsStr(ids, toString(project["id"])) {
		ids = append(ids, toString(project["id"]))
		space["projects"] = ids
		space["edited"] = float64(timestamp())
		putDocument(spacesFile, toString(space["_id"]), space)
		addNotification(toString(space["owner"]), c.getString("username"), "space_project", Obj{
			"spaceId":    space["_id"],
			"spaceTitle": space["title"],
			"projectId":  project["id"],
			"projectTitle": project["title"],
		})
	}
	c.json(200, Obj{"ok": true, "space": spaceResponse(space, c)})
}

func handleRemoveSpaceProject(c *Context) {
	space, _, _ := collectionFindById(spacesFile, c.param("id"))
	if len(space) == 0 {
		c.notFound("space not found")
		return
	}
	projectRes := loadProject(c.param("project"))
	allowed := canManageSpace(space, c)
	if projectRes.isOk() {
		project := toObj(projectRes.unwrap())
		allowed = allowed || normalizeUsername(toString(project["owner"])) == normalizeUsername(c.getString("username"))
	}
	if !allowed {
		c.forbidden("you cannot remove this project")
		return
	}
	space["projects"] = removeStr(asArray(space["projects"]), c.param("project"))
	space["edited"] = float64(timestamp())
	putDocument(spacesFile, toString(space["_id"]), space)
	c.json(200, Obj{"ok": true, "space": spaceResponse(space, c)})
}

func handleFollowSpace(c *Context) {
	space, _, _ := collectionFindById(spacesFile, c.param("id"))
	if len(space) == 0 || !canViewSpace(space, c) {
		c.notFound("space not found")
		return
	}
	followers := asArray(space["followers"])
	if !stringArrayContainsUser(followers, c.getString("username")) {
		followers = append(followers, c.getString("username"))
	}
	space["followers"] = followers
	putDocument(spacesFile, toString(space["_id"]), space)
	c.json(200, Obj{"ok": true, "following": true, "followers": float64(len(followers))})
}

func handleUnfollowSpace(c *Context) {
	space, _, _ := collectionFindById(spacesFile, c.param("id"))
	if len(space) == 0 {
		c.notFound("space not found")
		return
	}
	space["followers"] = removeStr(asArray(space["followers"]), c.getString("username"))
	putDocument(spacesFile, toString(space["_id"]), space)
	c.json(200, Obj{"ok": true, "following": false, "followers": float64(len(asArray(space["followers"])))})
}

func handleMySpaces(c *Context) {
	username := c.getString("username")
	stored := loadCollection(spacesFile)
	collectionSort(stored, "edited", "descending")
	spaces := []any{}
	for _, it := range stored {
		space := toObj(it)
		related := normalizeUsername(toString(space["owner"])) == normalizeUsername(username) ||
			stringArrayContainsUser(spaceManagers(space), username) ||
			stringArrayContainsUser(asArray(space["followers"]), username)
		invites := spaceCuratorInvites(space)
		for _, inv := range invites {
			invite := toObj(inv)
			if normalizeUsername(toString(invite["username"])) == normalizeUsername(username) {
				related = true
			}
		}
		if related {
			spaces = append(spaces, spaceResponse(space, c))
		}
	}
	c.json(200, Obj{"ok": true, "spaces": spaces})
}

func handleGetSpaceManagement(c *Context) {
	space, _, _ := collectionFindById(spacesFile, c.param("id"))
	if len(space) == 0 {
		c.notFound("space not found")
		return
	}
	if !canManageSpace(space, c) {
		c.forbidden("you cannot manage this space")
		return
	}
	c.json(200, Obj{"ok": true, "space": spaceManagementResponse(space, c)})
}

func handleInviteSpaceCurator(c *Context) {
	space, _, _ := collectionFindById(spacesFile, c.param("id"))
	if len(space) == 0 {
		c.notFound("space not found")
		return
	}
	if !isSpaceOwner(space, c) {
		c.forbidden("only the owner can invite curators")
		return
	}
	username := strings.TrimSpace(toString(c.bodyJSON()["username"]))
	usernameKey := normalizeUsername(username)
	if usernameKey == "" || len(username) > 20 || usernameKey == normalizeUsername(toString(space["owner"])) {
		c.badRequest("choose another user")
		return
	}
	profile := loadProfile(usernameKey)
	if normalizeUsername(toString(profile["username"])) != usernameKey {
		c.notFound("user not found")
		return
	}
	if stringArrayContainsUser(spaceManagers(space), username) {
		c.badRequest("this user is already a curator")
		return
	}
	invites := spaceCuratorInvites(space)
	for _, it := range invites {
		existing := toObj(it)
		if normalizeUsername(toString(existing["username"])) == usernameKey {
			c.badRequest("this user already has an invitation")
			return
		}
	}
	invite := Obj{"username": toString(profile["username"]), "invitedBy": c.getString("username"), "created": float64(timestamp())}
	invites = append(invites, invite)
	space["curatorInvites"] = invites
	space["edited"] = float64(timestamp())
	putDocument(spacesFile, toString(space["_id"]), space)
	addNotification(toString(profile["username"]), c.getString("username"), "space_curator_invite", Obj{"spaceId": space["_id"], "spaceTitle": space["title"]})
	c.json(201, Obj{"ok": true, "invitation": invite})
}

func handleRespondSpaceCuratorInvite(c *Context) {
	space, _, _ := collectionFindById(spacesFile, c.param("id"))
	if len(space) == 0 {
		c.notFound("space not found")
		return
	}
	username := c.getString("username")
	invites := spaceCuratorInvites(space)
	remaining := []any{}
	found := false
	for _, it := range invites {
		invite := toObj(it)
		if normalizeUsername(toString(invite["username"])) == normalizeUsername(username) {
			found = true
		} else {
			remaining = append(remaining, invite)
		}
	}
	if !found {
		c.notFound("invitation not found")
		return
	}
	accepted := toBool(c.bodyJSON()["accepted"])
	if accepted {
		managers := spaceManagers(space)
		if !stringArrayContainsUser(managers, username) {
			managers = append(managers, username)
		}
		space["managers"] = managers
	}
	space["curatorInvites"] = remaining
	space["edited"] = float64(timestamp())
	putDocument(spacesFile, toString(space["_id"]), space)
	notificationType := "space_curator_declined"
	if accepted {
		notificationType = "space_curator_accepted"
	}
	addNotification(toString(space["owner"]), username, notificationType, Obj{"spaceId": space["_id"], "spaceTitle": space["title"]})
	c.json(200, Obj{"ok": true, "accepted": accepted})
}

func handleRemoveSpaceCurator(c *Context) {
	space, _, _ := collectionFindById(spacesFile, c.param("id"))
	if len(space) == 0 {
		c.notFound("space not found")
		return
	}
	if !isSpaceOwner(space, c) {
		c.forbidden("only the owner can remove curators")
		return
	}
	username := c.param("username")
	space["managers"] = removeStr(spaceManagers(space), username)
	space["edited"] = float64(timestamp())
	putDocument(spacesFile, toString(space["_id"]), space)
	addNotification(username, c.getString("username"), "space_curator_removed", Obj{"spaceId": space["_id"], "spaceTitle": space["title"]})
	c.json(200, Obj{"ok": true})
}

func handleCancelSpaceCuratorInvite(c *Context) {
	space, _, _ := collectionFindById(spacesFile, c.param("id"))
	if len(space) == 0 {
		c.notFound("space not found")
		return
	}
	if !isSpaceOwner(space, c) {
		c.forbidden("only the owner can cancel invitations")
		return
	}
	username := c.param("username")
	remaining := []any{}
	invites := spaceCuratorInvites(space)
	for _, it := range invites {
		invite := toObj(it)
		if normalizeUsername(toString(invite["username"])) != normalizeUsername(username) {
			remaining = append(remaining, invite)
		}
	}
	space["curatorInvites"] = remaining
	space["edited"] = float64(timestamp())
	putDocument(spacesFile, toString(space["_id"]), space)
	c.json(200, Obj{"ok": true})
}

func handleReactSpace(c *Context) {
	space, _, _ := collectionFindById(spacesFile, c.param("id"))
	if len(space) == 0 || !canViewSpace(space, c) {
		c.notFound("space not found")
		return
	}
	reactionType := toString(c.bodyJSON()["type"])
	if !isValidReaction(reactionType) {
		c.badRequest("invalid reaction")
		return
	}
	username := c.getString("username")
	likes := spaceLikes(space)
	brokenHearts := spaceBrokenHearts(space)
	hadReaction := false
	if reactionType == "heart" {
		hadReaction = stringArrayContainsUser(likes, username)
	} else {
		hadReaction = stringArrayContainsUser(brokenHearts, username)
	}
	likes = removeStr(likes, username)
	brokenHearts = removeStr(brokenHearts, username)
	if !hadReaction && reactionType == "heart" {
		likes = append(likes, username)
	} else if !hadReaction && reactionType == "brokenheart" {
		brokenHearts = append(brokenHearts, username)
	}
	space["likes"] = likes
	space["brokenHearts"] = brokenHearts
	space["edited"] = float64(timestamp())
	putDocument(spacesFile, toString(space["_id"]), space)
	myReaction := ""
	if !hadReaction {
		myReaction = reactionType
	}
	c.json(200, Obj{"ok": true, "likeCount": float64(len(likes)), "brokenHeartCount": float64(len(brokenHearts)), "myReaction": myReaction})
}

func handleGetSpaceComments(c *Context) {
	space, _, _ := collectionFindById(spacesFile, c.param("id"))
	if len(space) == 0 || !canViewSpace(space, c) {
		c.notFound("space not found")
		return
	}
	c.json(200, Obj{"ok": true, "comments": loadComments(spaceCommentKey(toString(space["_id"])))})
}

func handleCreateSpaceComment(c *Context) {
	space, _, _ := collectionFindById(spacesFile, c.param("id"))
	if len(space) == 0 || !canViewSpace(space, c) {
		c.notFound("space not found")
		return
	}
	body := c.bodyJSON()
	if body["content"] == nil {
		c.badRequest("comment content is required")
		return
	}
	parentId := ""
	if body["parent"] != nil {
		parentId = toString(body["parent"])
	}
	created := createComment(spaceCommentKey(toString(space["_id"])), c.getString("username"), toString(body["content"]), parentId)
	if !toBool(created["ok"]) {
		c.badRequest(toString(created["error"]))
		return
	}
	commenter := c.getString("username")
	parentAuthor := toString(created["parentAuthor"])
	newCommentId := toString(toObj(created["comment"])["id"])
	notificationData := Obj{"spaceId": space["_id"], "spaceTitle": space["title"], "commentId": newCommentId}
	if parentAuthor != "" && normalizeUsername(parentAuthor) != normalizeUsername(commenter) {
		addNotification(parentAuthor, commenter, "reply", notificationData)
	}
	if normalizeUsername(toString(space["owner"])) != normalizeUsername(commenter) && normalizeUsername(toString(space["owner"])) != normalizeUsername(parentAuthor) {
		addNotification(toString(space["owner"]), commenter, "space_comment", notificationData)
	}
	notifyMentions(toString(toObj(created["comment"])["content"]), commenter, toString(space["owner"]), notificationData)
	c.json(201, created)
}

func handleDeleteSpaceComment(c *Context) {
	space, _, _ := collectionFindById(spacesFile, c.param("id"))
	if len(space) == 0 {
		c.notFound("space not found")
		return
	}
	moderator := toString(space["owner"])
	if canManageSpace(space, c) {
		moderator = c.getString("username")
	}
	deleted := deleteComment(spaceCommentKey(toString(space["_id"])), c.param("cid"), c.getString("username"), moderator, c.getBool("isAdmin"))
	if !toBool(deleted["ok"]) {
		c.json(int(toFloat(deleted["status"])), Obj{"ok": false, "error": deleted["error"]})
		return
	}
	c.json(200, Obj{"ok": true})
}

func handleReactSpaceComment(c *Context) {
	space, _, _ := collectionFindById(spacesFile, c.param("id"))
	if len(space) == 0 || !canViewSpace(space, c) {
		c.notFound("space not found")
		return
	}
	reactToComment(c, spaceCommentKey(toString(space["_id"])))
}

// ---- Preview handlers ----

func handleCreatePreview(c *Context) {
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
	hours := float64(24)
	if body["hours"] != nil {
		hours = clampNumber(toFloat(body["hours"]), 1, 168)
	}
	key := randomString(24)
	previewId := toString(project["id"]) + ":" + key
	preview := Obj{
		"_id":       previewId,
		"projectId": project["id"],
		"key":       key,
		"owner":     c.getString("username"),
		"created":   float64(timestamp()),
		"expiresAt": float64(timestamp()) + (hours * 3600000),
	}
	collectionInsertOne(previewsFile, preview)
	c.json(201, Obj{"ok": true, "key": key, "expiresAt": preview["expiresAt"]})
}

// ---- Release handlers ----

func handleListReleases(c *Context) {
	result := loadProject(c.param("id"))
	if !result.isOk() || !canViewProject(toObj(result.unwrap()), c) {
		c.notFound("project not found")
		return
	}
	items := collectionWhere(releasesIndexFile, "projectId", "equal", c.param("id"))
	collectionSort(items, "created", "descending")
	c.json(200, Obj{"ok": true, "releases": items})
}

func handleCreateRelease(c *Context) {
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
	version := strings.TrimSpace(toString(body["version"]))
	channel := strings.ToLower(strings.TrimSpace(toString(body["channel"])))
	if version == "" || len(version) > 50 {
		c.badRequest("invalid version")
		return
	}
	if channel != "stable" && channel != "beta" && channel != "development" {
		c.badRequest("invalid release channel")
		return
	}
	notes := ""
	if body["notes"] != nil {
		notes = strings.TrimSpace(toString(body["notes"]))
	}
	if len(notes) > 10000 {
		c.badRequest("release notes are too long")
		return
	}
	releaseId := toString(project["id"]) + ":" + randomString(12)
	commit := ""
	if body["commit"] != nil {
		commit = toString(body["commit"])
	}
	sourceUrl := projectJsonBaseUrl(project)
	if projectViewKey(project) != "" {
		sourceUrl = sourceUrl + "?k=" + projectViewKey(project)
	}
	snapshot := requestsGet(sourceUrl, nil)
	if !snapshot.success {
		c.internalError("could not snapshot the current project")
		return
	}
	releasePath := releasesDir + strings.ReplaceAll(releaseId, ":", "-") + ".json"
	if !fsWriteFile(releasePath, snapshot.body) {
		c.internalError("could not store the release")
		return
	}
	jsonUrl := appURL + "/api/projects/" + toString(project["id"]) + "/releases/" + urlEscape(releaseId) + "/project.json"
	release := Obj{
		"_id":       releaseId,
		"projectId": project["id"],
		"version":   version,
		"channel":   channel,
		"notes":     notes,
		"commit":    commit,
		"jsonUrl":   jsonUrl,
		"assetsBase": assetsBaseUrl(),
		"created":   float64(timestamp()),
		"creator":   c.getString("username"),
	}
	collectionInsertOne(releasesIndexFile, release)
	if channel == "stable" {
		project["publicRelease"] = releaseId
		saveProject(project)
	}
	addActivity(c.getString("username"), "release", Obj{"projectId": project["id"], "projectTitle": project["title"], "version": version})
	c.json(201, Obj{"ok": true, "release": release})
}

func handleGetReleaseProjectJson(c *Context) {
	release, _, _ := collectionFindById(releasesIndexFile, c.param("release"))
	if len(release) == 0 || toString(release["projectId"]) != c.param("id") {
		c.notFound("release not found")
		return
	}
	result := loadProject(c.param("id"))
	if !result.isOk() || !canViewProject(toObj(result.unwrap()), c) {
		c.notFound("release not found")
		return
	}
	releasePath := releasesDir + strings.ReplaceAll(toString(release["_id"]), ":", "-") + ".json"
	if !fsExists(releasePath) {
		c.notFound("release snapshot not found")
		return
	}
	c.setHeader("Content-Type", "application/json")
	c.text(200, fsReadFile(releasePath))
}

// ---- Contribution handler ----

func handleCreateContribution(c *Context) {
	parentResult := loadProject(c.param("id"))
	body := c.bodyJSON()
	remixResult := loadProject(toString(body["remixProjectId"]))
	if !parentResult.isOk() || !remixResult.isOk() {
		c.notFound("project not found")
		return
	}
	parent := toObj(parentResult.unwrap())
	remix := toObj(remixResult.unwrap())
	if !projectHasRepo(parent) || !projectHasRepo(remix) {
		c.badRequest("both projects need a repository")
		return
	}
	if normalizeUsername(toString(remix["owner"])) != normalizeUsername(c.getString("username")) || toString(remix["remixParent"]) != toString(parent["id"]) {
		c.forbidden("use a remix you own")
		return
	}
	title := strings.TrimSpace(toString(body["title"]))
	if title == "" || len(title) > 200 {
		c.badRequest("invalid title")
		return
	}
	parentRepo := toObj(parent["repo"])
	remixRepo := toObj(remix["repo"])
	head := toString(remixRepo["owner"]) + ":" + toString(remixRepo["defaultBranch"])
	description := ""
	if body["body"] != nil {
		description = toString(body["body"])
	}
	resp := giteaPost(repoApiPath(parent)+"/pulls", Obj{"title": title, "body": description, "head": head, "base": parentRepo["defaultBranch"]}, c.getString("username"))
	parsed := parseGiteaObject(resp)
	if !toBool(parsed["ok"]) {
		c.json(502, Obj{"ok": false, "error": "could not send contribution"})
		return
	}
	addNotification(toString(parent["owner"]), c.getString("username"), "contribution", Obj{"projectId": parent["id"], "projectTitle": parent["title"]})
	c.json(201, Obj{"ok": true, "pull": slimPull(toObj(parsed["data"]))})
}

// ---- Diagnostic handlers ----

func handleRecordDiagnostic(c *Context) {
	result := loadProject(c.param("id"))
	if !result.isOk() || !projectIsViewable(toObj(result.unwrap())) {
		c.notFound("project not found")
		return
	}
	body := c.bodyJSON()
	eventType := strings.ToLower(strings.TrimSpace(toString(body["type"])))
	if eventType != "load" && eventType != "start" && eventType != "crash" && eventType != "exit" && eventType != "playtime" {
		c.badRequest("invalid diagnostic type")
		return
	}
	diagnosticId := c.param("id") + ":" + randomString(16)
	duration := float64(0)
	loadMs := float64(0)
	errorText := ""
	device := ""
	if body["duration"] != nil {
		duration = clampNumber(toFloat(body["duration"]), 0, 86400000)
	}
	if body["loadMs"] != nil {
		loadMs = clampNumber(toFloat(body["loadMs"]), 0, 600000)
	}
	if body["error"] != nil {
		errorText = trimLen(toString(body["error"]), 1, 500)
	}
	if body["device"] != nil {
		device = trimLen(toString(body["device"]), 1, 40)
	}
	if eventType == "playtime" {
		if !c.getBool("authenticated") || duration <= 0 {
			c.json(201, Obj{"ok": true})
			return
		}
		playtimeId := c.param("id") + ":" + normalizeUsername(c.getString("username"))
		playtime, _, _ := collectionFindById(playtimeFile, playtimeId)
		if len(playtime) == 0 {
			playtime = Obj{"_id": playtimeId, "projectId": c.param("id"), "username": c.getString("username"), "duration": float64(0), "updated": float64(timestamp())}
		}
		playtime["duration"] = toFloat(playtime["duration"]) + clampNumber(duration, 0, 21600000)
		playtime["updated"] = float64(timestamp())
		putDocument(playtimeFile, playtimeId, playtime)
		c.json(201, Obj{"ok": true})
		return
	}
	item := Obj{
		"_id":       diagnosticId,
		"projectId": c.param("id"),
		"type":      eventType,
		"duration":  duration,
		"loadMs":    loadMs,
		"error":     errorText,
		"device":    device,
		"created":   float64(timestamp()),
	}
	collectionInsertOne(diagnosticsFile, item)
	c.json(201, Obj{"ok": true})
}

func handleGetDiagnostics(c *Context) {
	result := loadProject(c.param("id"))
	if !result.isOk() || !canEditProject(toObj(result.unwrap()), c) {
		c.forbidden("not your project")
		return
	}
	items := collectionWhere(diagnosticsFile, "projectId", "equal", c.param("id"))
	collectionSort(items, "created", "descending")
	items = collectionLimit(items, 500)
	counts := Obj{"load": float64(0), "start": float64(0), "crash": float64(0), "exit": float64(0)}
	totalLoadMs := float64(0)
	loadSamples := float64(0)
	for _, it := range items {
		item := toObj(it)
		kind := toString(item["type"])
		counts[kind] = toFloat(counts[kind]) + 1
		if toFloat(item["loadMs"]) > 0 {
			totalLoadMs += toFloat(item["loadMs"])
			loadSamples++
		}
	}
	averageLoadMs := float64(0)
	if loadSamples > 0 {
		averageLoadMs = totalLoadMs / loadSamples
	}
	c.json(200, Obj{"ok": true, "counts": counts, "averageLoadMs": averageLoadMs, "recent": paginate(items, 0, 50)})
}

// ---- Feedback handlers ----

func handleCreateProjectFeedback(c *Context) {
	result := loadProject(c.param("id"))
	if !result.isOk() || !canViewProject(toObj(result.unwrap()), c) {
		c.notFound("project not found")
		return
	}
	project := toObj(result.unwrap())
	body := c.bodyJSON()
	feedbackType := strings.ToLower(strings.TrimSpace(toString(body["type"])))
	if feedbackType != "bug" && feedbackType != "idea" && feedbackType != "confusing" && feedbackType != "other" {
		c.badRequest("invalid feedback type")
		return
	}
	message := strings.TrimSpace(toString(body["message"]))
	if message == "" || len(message) > 2000 {
		c.badRequest("feedback must be between 1 and 2000 characters")
		return
	}
	feedbackId := toString(project["id"]) + ":" + randomString(16)
	feedback := Obj{
		"_id":       feedbackId,
		"projectId": project["id"],
		"type":      feedbackType,
		"message":   message,
		"status":    "open",
		"author":    c.getString("username"),
		"created":   float64(timestamp()),
		"edited":    float64(timestamp()),
	}
	collectionInsertOne(feedbackFile, feedback)
	addNotification(toString(project["owner"]), c.getString("username"), "project_feedback", Obj{"projectId": project["id"], "projectTitle": project["title"], "feedbackType": feedbackType})
	c.json(201, Obj{"ok": true})
}

func handleGetProjectFeedback(c *Context) {
	result := loadProject(c.param("id"))
	if !result.isOk() || !canEditProject(toObj(result.unwrap()), c) {
		c.forbidden("not your project")
		return
	}
	items := collectionWhere(feedbackFile, "projectId", "equal", c.param("id"))
	collectionSort(items, "created", "descending")
	items = collectionLimit(items, 200)
	c.json(200, Obj{"ok": true, "feedback": items})
}

func handleUpdateProjectFeedback(c *Context) {
	result := loadProject(c.param("id"))
	if !result.isOk() || !canEditProject(toObj(result.unwrap()), c) {
		c.forbidden("not your project")
		return
	}
	feedback, _, _ := collectionFindById(feedbackFile, c.param("feedback"))
	if len(feedback) == 0 || toString(feedback["projectId"]) != c.param("id") {
		c.notFound("feedback not found")
		return
	}
	status := strings.ToLower(toString(c.bodyJSON()["status"]))
	if status != "open" && status != "working" && status != "done" && status != "dismissed" {
		c.badRequest("invalid feedback status")
		return
	}
	feedback["status"] = status
	feedback["edited"] = float64(timestamp())
	putDocument(feedbackFile, toString(feedback["_id"]), feedback)
	c.json(200, Obj{"ok": true, "feedback": feedback})
}

// ---- Review handlers ----

func handleListProjectReviews(c *Context) {
	result := loadProject(c.param("id"))
	if !result.isOk() || !canViewProject(toObj(result.unwrap()), c) {
		c.notFound("project not found")
		return
	}
	items := collectionWhere(reviewsFile, "projectId", "equal", c.param("id"))
	collectionSort(items, "edited", "descending")
	items = collectionLimit(items, 200)
	ratingTotal := float64(0)
	myReview := Obj{}
	username := c.getString("username")
	for i, it := range items {
		review := toObj(it)
		ratingTotal += toFloat(review["rating"])
		review["playtimeMs"] = projectPlaytime(c.param("id"), toString(review["author"]))
		if normalizeUsername(toString(review["author"])) == normalizeUsername(username) {
			myReview = review
		}
		items[i] = review
	}
	average := float64(0)
	if len(items) > 0 {
		average = ratingTotal / float64(len(items))
	}
	c.json(200, Obj{"ok": true, "reviews": items, "count": float64(len(items)), "average": average, "myReview": myReview})
}

func handleListUserReviews(c *Context) {
	name := normalizeUsername(c.param("name"))
	allReviews := loadCollection(reviewsFile)
	collectionSort(allReviews, "edited", "descending")
	allReviews = collectionLimit(allReviews, 1000)
	reviews := []any{}
	for _, it := range allReviews {
		review := toObj(it)
		if normalizeUsername(toString(review["author"])) == name && len(reviews) < 100 {
			r := loadProject(toString(review["projectId"]))
			if r.isOk() && canViewProject(toObj(r.unwrap()), c) {
				project := toObj(r.unwrap())
				review["projectTitle"] = project["title"]
				review["projectOwner"] = project["owner"]
				review["playtimeMs"] = projectPlaytime(toString(review["projectId"]), toString(review["author"]))
				reviews = append(reviews, review)
			}
		}
	}
	c.json(200, Obj{"ok": true, "reviews": reviews})
}

func handleSaveProjectReview(c *Context) {
	result := loadProject(c.param("id"))
	if !result.isOk() || !canViewProject(toObj(result.unwrap()), c) {
		c.notFound("project not found")
		return
	}
	project := toObj(result.unwrap())
	if normalizeUsername(toString(project["owner"])) == normalizeUsername(c.getString("username")) {
		c.badRequest("you cannot review your own project")
		return
	}
	body := c.bodyJSON()
	rating := toFloat(body["rating"])
	if rating < 1 || rating > 5 {
		c.badRequest("rating must be between 1 and 5")
		return
	}
	message := ""
	if body["message"] != nil {
		message = strings.TrimSpace(toString(body["message"]))
	}
	if len(message) > 2000 {
		c.badRequest("review is too long")
		return
	}
	reviewId := toString(project["id"]) + ":" + normalizeUsername(c.getString("username"))
	existing, _, _ := collectionFindById(reviewsFile, reviewId)
	created := float64(timestamp())
	if len(existing) > 0 {
		created = toFloat(existing["created"])
	}
	review := Obj{
		"_id":       reviewId,
		"projectId": project["id"],
		"author":    c.getString("username"),
		"rating":    rating,
		"message":   message,
		"created":   created,
		"edited":    float64(timestamp()),
	}
	putDocument(reviewsFile, reviewId, review)
	if len(existing) == 0 {
		addNotification(toString(project["owner"]), c.getString("username"), "project_review", Obj{"projectId": project["id"], "projectTitle": project["title"], "rating": rating})
	}
	review["playtimeMs"] = projectPlaytime(toString(project["id"]), toString(review["author"]))
	c.json(200, Obj{"ok": true, "review": review})
}

func handleDeleteProjectReview(c *Context) {
	result := loadProject(c.param("id"))
	if !result.isOk() || !canViewProject(toObj(result.unwrap()), c) {
		c.notFound("project not found")
		return
	}
	reviewId := c.param("id") + ":" + normalizeUsername(c.getString("username"))
	collectionDeleteOne(reviewsFile, reviewId)
	c.json(200, Obj{"ok": true})
}

// ---- Idea / Roadmap handlers ----

func handleListIdeas(c *Context) {
	ideas := loadCollection(ideasFile)
	collectionSort(ideas, "created", "descending")
	username := c.getString("username")
	for i, it := range ideas {
		idea := toObj(it)
		likes := asArray(idea["likes"])
		dislikes := asArray(idea["dislikes"])
		idea["likesCount"] = float64(len(likes))
		idea["dislikesCount"] = float64(len(dislikes))
		idea["score"] = float64(len(likes) - len(dislikes))
		myVote := ""
		if stringArrayContainsUser(likes, username) {
			myVote = "like"
		} else if stringArrayContainsUser(dislikes, username) {
			myVote = "dislike"
		}
		idea["myVote"] = myVote
		if !objHas(idea, "source") {
			idea["source"] = "community"
			if normalizeUsername(toString(idea["author"])) == "mist" {
				idea["source"] = "mistwarp"
			}
		}
		if !objHas(idea, "interested") {
			idea["interested"] = toString(idea["source"]) == "mistwarp"
		}
		idea["commentCount"] = float64(len(loadComments("roadmap-" + toString(idea["_id"]))))
		idea["likes"] = []any{}
		idea["dislikes"] = []any{}
		ideas[i] = idea
	}
	sortObjBy(ideas, "score", "descending")
	c.json(200, Obj{"ok": true, "ideas": ideas})
}

func handleCreateIdea(c *Context) {
	body := c.bodyJSON()
	title := strings.TrimSpace(toString(body["title"]))
	description := strings.TrimSpace(toString(body["description"]))
	if title == "" || len(title) > 120 || description == "" || len(description) > 3000 {
		c.badRequest("add a title and a description")
		return
	}
	category := "Other"
	if body["category"] != nil {
		category = trimLen(toString(body["category"]), 1, 40)
	}
	ideaId := "i" + randomString(14)
	source := "community"
	if c.getBool("isAdmin") {
		source = "mistwarp"
	}
	idea := Obj{
		"_id":         ideaId,
		"title":       title,
		"description": description,
		"category":    category,
		"author":      c.getString("username"),
		"source":      source,
		"interested":  source == "mistwarp",
		"status":      "open",
		"likes":       []any{c.getString("username")},
		"dislikes":    []any{},
		"votes":       float64(1),
		"created":     float64(timestamp()),
		"edited":      float64(timestamp()),
	}
	collectionInsertOne(ideasFile, idea)
	idea["myVote"] = "like"
	idea["likesCount"] = float64(1)
	idea["dislikesCount"] = float64(0)
	idea["score"] = float64(1)
	idea["commentCount"] = float64(0)
	idea["likes"] = []any{}
	idea["dislikes"] = []any{}
	c.json(201, Obj{"ok": true, "idea": idea})
}

func handleVoteIdea(c *Context) {
	idea, _, _ := collectionFindById(ideasFile, c.param("id"))
	if len(idea) == 0 {
		c.notFound("idea not found")
		return
	}
	username := c.getString("username")
	vote := strings.ToLower(toString(c.bodyJSON()["vote"]))
	if vote != "like" && vote != "dislike" {
		c.badRequest("vote must be like or dislike")
		return
	}
	likes := asArray(idea["likes"])
	dislikes := asArray(idea["dislikes"])
	hadVote := false
	if vote == "like" {
		hadVote = stringArrayContainsUser(likes, username)
	} else {
		hadVote = stringArrayContainsUser(dislikes, username)
	}
	likes = removeStr(likes, username)
	dislikes = removeStr(dislikes, username)
	if !hadVote && vote == "like" {
		likes = append(likes, username)
	} else if !hadVote && vote == "dislike" {
		dislikes = append(dislikes, username)
	}
	idea["likes"] = likes
	idea["dislikes"] = dislikes
	idea["votes"] = float64(len(likes) - len(dislikes))
	idea["edited"] = float64(timestamp())
	putDocument(ideasFile, toString(idea["_id"]), idea)
	myVote := ""
	if !hadVote {
		myVote = vote
	}
	c.json(200, Obj{"ok": true, "myVote": myVote, "likesCount": float64(len(likes)), "dislikesCount": float64(len(dislikes)), "score": float64(len(likes) - len(dislikes))})
}

func handleGetIdeaComments(c *Context) {
	idea, _, _ := collectionFindById(ideasFile, c.param("id"))
	if len(idea) == 0 {
		c.notFound("idea not found")
		return
	}
	c.json(200, Obj{"ok": true, "comments": loadComments("roadmap-" + toString(idea["_id"]))})
}

func handleCreateIdeaComment(c *Context) {
	idea, _, _ := collectionFindById(ideasFile, c.param("id"))
	if len(idea) == 0 {
		c.notFound("idea not found")
		return
	}
	body := c.bodyJSON()
	if body["content"] == nil {
		c.badRequest("comment content is required")
		return
	}
	parentId := ""
	if body["parent"] != nil {
		parentId = toString(body["parent"])
	}
	created := createComment("roadmap-"+toString(idea["_id"]), c.getString("username"), toString(body["content"]), parentId)
	if !toBool(created["ok"]) {
		c.badRequest(toString(created["error"]))
		return
	}
	commenter := c.getString("username")
	parentAuthor := toString(created["parentAuthor"])
	if parentAuthor != "" && normalizeUsername(parentAuthor) != normalizeUsername(commenter) {
		addNotification(parentAuthor, commenter, "roadmap_comment", Obj{"roadmapId": idea["_id"], "roadmapTitle": idea["title"]})
	}
	if normalizeUsername(toString(idea["author"])) != normalizeUsername(parentAuthor) {
		addNotification(toString(idea["author"]), commenter, "roadmap_comment", Obj{"roadmapId": idea["_id"], "roadmapTitle": idea["title"]})
	}
	notifyMentions(toString(toObj(created["comment"])["content"]), commenter, toString(idea["author"]), Obj{"roadmapId": idea["_id"], "roadmapTitle": idea["title"]})
	c.json(201, created)
}

func handleDeleteIdeaComment(c *Context) {
	idea, _, _ := collectionFindById(ideasFile, c.param("id"))
	if len(idea) == 0 {
		c.notFound("idea not found")
		return
	}
	deleted := deleteComment("roadmap-"+toString(idea["_id"]), c.param("cid"), c.getString("username"), toString(idea["author"]), c.getBool("isAdmin"))
	if !toBool(deleted["ok"]) {
		c.json(int(toFloat(deleted["status"])), Obj{"ok": false, "error": deleted["error"]})
		return
	}
	c.json(200, Obj{"ok": true})
}

func handleUpdateIdea(c *Context) {
	idea, _, _ := collectionFindById(ideasFile, c.param("id"))
	if len(idea) == 0 {
		c.notFound("idea not found")
		return
	}
	body := c.bodyJSON()
	if body["status"] != nil {
		status := strings.ToLower(toString(body["status"]))
		if status == "open" || status == "planned" || status == "building" || status == "shipped" || status == "declined" {
			idea["status"] = status
		}
	}
	if body["interested"] != nil {
		idea["interested"] = toBool(body["interested"])
	}
	idea["edited"] = float64(timestamp())
	putDocument(ideasFile, toString(idea["_id"]), idea)
	c.json(200, Obj{"ok": true, "idea": idea})
}

// ---- XML escape helper ----

func xmlEscape(value string) string {
	value = strings.ReplaceAll(value, "&", "&amp;")
	value = strings.ReplaceAll(value, "<", "&lt;")
	value = strings.ReplaceAll(value, ">", "&gt;")
	value = strings.ReplaceAll(value, "\"", "&quot;")
	return value
}

// ---- Project Card handler ----

func handleProjectCard(c *Context) {
	result := loadProject(c.param("id"))
	if !result.isOk() || !projectIsViewable(toObj(result.unwrap())) {
		c.notFound("project not found")
		return
	}
	project := toObj(result.unwrap())
	title := trimLen(xmlEscape(toString(project["title"])), 1, 58)
	owner := trimLen(xmlEscape(toString(project["owner"])), 1, 30)
	thumbnail := xmlEscape(thumbUrlFor(project))
	hearts := float64(len(asArray(project["loves"])))
	svg := "<svg xmlns=\"http://www.w3.org/2000/svg\" width=\"1200\" height=\"630\" viewBox=\"0 0 1200 630\"><rect width=\"1200\" height=\"630\" fill=\"#17151d\"/><rect x=\"48\" y=\"48\" width=\"650\" height=\"488\" rx=\"24\" fill=\"#24202d\"/><image href=\"" + thumbnail + "\" x=\"48\" y=\"48\" width=\"650\" height=\"488\" preserveAspectRatio=\"xMidYMid slice\"/><text x=\"750\" y=\"190\" fill=\"#a78bfa\" font-family=\"Arial,sans-serif\" font-size=\"34\" font-weight=\"700\">MistWarp</text><text x=\"750\" y=\"270\" fill=\"#ffffff\" font-family=\"Arial,sans-serif\" font-size=\"48\" font-weight=\"700\">" + title + "</text><text x=\"750\" y=\"330\" fill=\"#c8c2d2\" font-family=\"Arial,sans-serif\" font-size=\"28\">by " + owner + "</text><text x=\"750\" y=\"430\" fill=\"#c8c2d2\" font-family=\"Arial,sans-serif\" font-size=\"24\">" + toString(project["views"]) + " plays   " + formatFloat(hearts) + " hearts</text></svg>"
	c.setHeader("Content-Type", "image/svg+xml")
	c.setHeader("Cache-Control", "public, max-age=300")
	c.text(200, svg)
}

// ---- projectPlaytime (from database.osl) ----

func projectPlaytime(projectId, username string) float64 {
	if username == "" {
		return 0
	}
	entry, _, _ := collectionFindById(playtimeFile, projectId+":"+normalizeUsername(username))
	if len(entry) == 0 {
		return 0
	}
	return toFloat(entry["duration"])
}

// ---- trimLen helper ----

func trimLen(s string, min, max int) string {
	if len(s) < min {
		return s
	}
	if len(s) > max {
		return s[:max]
	}
	return s
}
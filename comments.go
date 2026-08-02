package main

import "strings"

// comments.go — maps comments.osl.

func createComment(key, author, content, parentId string) Obj {
	trimmed := strings.TrimSpace(content)
	if trimmed == "" {
		return Obj{"ok": false, "error": "comment is empty"}
	}
	if len(trimmed) > 500 {
		return Obj{"ok": false, "error": "comment too long"}
	}
	comments := loadComments(key)

	parentAuthor := ""
	if parentId != "" {
		parentOk := false
		for _, it := range comments {
			existing := toObj(it)
			if toString(existing["id"]) == parentId && toString(existing["parent"]) == "" {
				parentOk = true
				parentAuthor = toString(existing["author"])
			}
		}
		if !parentOk {
			return Obj{"ok": false, "error": "invalid parent comment"}
		}
	}

	comment := Obj{
		"id":      generateCommentId(),
		"author":  author,
		"content": trimmed,
		"created": timestamp(),
		"parent":  parentId,
	}
	if fsExists(commentsDir + key + ".jsonl") {
		appendComment(key, comment)
	} else {
		comments = append(comments, comment)
		saveComments(key, comments)
	}
	return Obj{"ok": true, "comment": comment, "parentAuthor": parentAuthor}
}

func notifyMentions(content, actor, skip string, extra Obj) {
	tokens := regexFindAll("(^|[^A-Za-z0-9_])@[A-Za-z0-9_]+", content)
	notified := []any{normalizeUsername(skip)}
	for _, it := range tokens {
		parts := strings.Split(toString(it), "@")
		name := ""
		if len(parts) >= 2 {
			name = normalizeUsername(parts[1])
		}
		if !containsStr(notified, name) && isSafePathPart(name) && fsExists(usersDir+name+".json") {
			addNotification(name, actor, "mention", extra)
			notified = append(notified, name)
		}
	}
}

func deleteComment(key, cid, requester, moderator string, isAdmin bool) Obj {
	comments := loadComments(key)
	target := Obj{}
	found := false
	for _, it := range comments {
		existing := toObj(it)
		if toString(existing["id"]) == cid {
			target = existing
			found = true
		}
	}
	if !found {
		return Obj{"ok": false, "error": "comment not found", "status": float64(404)}
	}

	requesterKey := normalizeUsername(requester)
	allowed := isAdmin
	allowed = allowed || requesterKey == normalizeUsername(toString(target["author"]))
	allowed = allowed || requesterKey == normalizeUsername(moderator)
	if !allowed {
		return Obj{"ok": false, "error": "not allowed", "status": float64(403)}
	}

	next := []any{}
	for _, it := range comments {
		existing := toObj(it)
		if toString(existing["id"]) != cid && toString(existing["parent"]) != cid {
			next = append(next, existing)
		}
	}
	saveComments(key, next)
	return Obj{"ok": true}
}

func handleGetProjectComments(c *Context) {
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
	c.json(200, Obj{"ok": true, "comments": loadComments("project-" + toString(project["id"]))})
}

func handleCreateProjectComment(c *Context) {
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
	if !canInteractProject(project, c) {
		c.json(403, Obj{"ok": false, "error": "buy this project to comment on it"})
		return
	}
	if project["commentsOff"] == true {
		c.json(403, Obj{"ok": false, "error": "comments are disabled on this project"})
		return
	}

	body := c.bodyJSON()
	if body["content"] == nil {
		c.badRequest("comment content is required")
		return
	}
	parentId := toString(body["parent"])

	created := createComment("project-"+toString(project["id"]), c.getString("username"), toString(body["content"]), parentId)
	if !toBool(created["ok"]) {
		c.badRequest(toString(created["error"]))
		return
	}
	commenter := c.getString("username")
	parentAuthor := toString(created["parentAuthor"])
	newCommentId := toString(toObj(created["comment"])["id"])
	if parentAuthor != "" && normalizeUsername(parentAuthor) != normalizeUsername(commenter) {
		addNotification(parentAuthor, commenter, "reply", Obj{"projectId": project["id"], "projectTitle": project["title"], "commentId": newCommentId})
	}
	if normalizeUsername(toString(project["owner"])) != normalizeUsername(parentAuthor) {
		addNotification(toString(project["owner"]), commenter, "comment", Obj{"projectId": project["id"], "projectTitle": project["title"], "commentId": newCommentId})
	}
	notifyMentions(toString(toObj(created["comment"])["content"]), commenter, toString(project["owner"]), Obj{"projectId": project["id"], "projectTitle": project["title"], "commentId": newCommentId})
	c.json(201, created)
}

func handleDeleteProjectComment(c *Context) {
	res := loadProject(c.param("id"))
	if !res.isOk() {
		c.notFound("project not found")
		return
	}
	project := toObj(res.unwrap())
	requesterIsAdmin := c.getBool("isAdmin")
	deleted := deleteComment("project-"+toString(project["id"]), c.param("cid"), c.getString("username"), toString(project["owner"]), requesterIsAdmin)
	if !toBool(deleted["ok"]) {
		c.json(int(toFloat(deleted["status"])), Obj{"ok": false, "error": deleted["error"]})
		return
	}
	c.json(200, Obj{"ok": true})
}

func profileCommentKey(name string) string { return "profile-" + normalizeUsername(name) }

func profileCommentsOff(name string) bool {
	profile := loadProfile(name)
	return profile["commentsOff"] == true
}

func handleGetProfileComments(c *Context) {
	name := c.param("name")
	c.json(200, Obj{"ok": true, "comments": loadComments(profileCommentKey(name)), "commentsOff": profileCommentsOff(name)})
}

func handleCreateProfileComment(c *Context) {
	name := c.param("name")
	nameKey := normalizeUsername(name)
	if !isSafePathPart(nameKey) || !fsExists(usersDir+nameKey+".json") {
		c.notFound("this user is not on Bilup")
		return
	}
	if profileCommentsOff(name) {
		c.json(403, Obj{"ok": false, "error": "comments are disabled on this profile"})
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

	created := createComment(profileCommentKey(name), c.getString("username"), toString(body["content"]), parentId)
	if !toBool(created["ok"]) {
		c.badRequest(toString(created["error"]))
		return
	}
	commenter := c.getString("username")
	parentAuthor := toString(created["parentAuthor"])
	newCommentId := toString(toObj(created["comment"])["id"])
	if parentAuthor != "" && normalizeUsername(parentAuthor) != normalizeUsername(commenter) {
		addNotification(parentAuthor, commenter, "reply", Obj{"profile": name, "commentId": newCommentId})
	}
	if normalizeUsername(name) != normalizeUsername(commenter) && normalizeUsername(name) != normalizeUsername(parentAuthor) {
		addNotification(name, commenter, "profile_comment", Obj{"profile": name, "commentId": newCommentId})
	}
	notifyMentions(toString(toObj(created["comment"])["content"]), commenter, name, Obj{"profile": name, "commentId": newCommentId})
	c.json(201, created)
}

func handleDeleteProfileComment(c *Context) {
	requesterIsAdmin := c.getBool("isAdmin")
	deleted := deleteComment(profileCommentKey(c.param("name")), c.param("cid"), c.getString("username"), c.param("name"), requesterIsAdmin)
	if !toBool(deleted["ok"]) {
		c.json(int(toFloat(deleted["status"])), Obj{"ok": false, "error": deleted["error"]})
		return
	}
	c.json(200, Obj{"ok": true})
}

func reactToComment(c *Context, key string) {
	body := c.bodyJSON()
	reactionType := toString(body["type"])
	if !isValidReaction(reactionType) {
		c.badRequest("invalid reaction")
		return
	}
	username := normalizeUsername(c.getString("username"))
	cid := c.param("cid")
	comments := loadComments(key)
	found := false
	for i, it := range comments {
		comment := toObj(it)
		if toString(comment["id"]) == cid {
			reactions := Obj{}
			if comment["reactions"] != nil {
				reactions = toObj(comment["reactions"])
			}
			comment["reactions"] = toggleReactionMap(reactions, username, reactionType)
			comments[i] = comment
			found = true
		}
	}
	if !found {
		c.notFound("comment not found")
		return
	}
	saveComments(key, comments)
	c.json(200, Obj{"ok": true})
}

func handleReactProjectComment(c *Context) {
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
	reactToComment(c, "project-"+toString(project["id"]))
}

func handleReactProfileComment(c *Context) {
	reactToComment(c, profileCommentKey(c.param("name")))
}
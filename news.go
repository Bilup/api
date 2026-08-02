package main

import "strings"

// news.go — maps news.osl.

func loadNews() []any {
	res := readFile(newsFile)
	if !res.isOk() {
		return []any{}
	}
	data := toObj(res.unwrap())
	return asArray(data["items"])
}

func saveNews(items []any) bool {
	return writeFile(newsFile, Obj{"items": items})
}

// toggleReactionMap removes the user from every reaction bucket, then (if they
// did not already hold this reaction) adds them to the chosen type.
func toggleReactionMap(reactions Obj, username, reactionType string) Obj {
	var chosen []any
	if objHas(reactions, reactionType) && reactions[reactionType] != nil {
		chosen = asArray(reactions[reactionType])
	}
	had := containsStr(chosen, username)
	for _, k := range objKeys(reactions) {
		var users []any
		if reactions[k] != nil {
			users = asArray(reactions[k])
		}
		reactions[k] = removeStr(users, username)
	}
	if !had {
		updated := []any{}
		if reactions[reactionType] != nil {
			updated = asArray(reactions[reactionType])
		}
		updated = append(updated, username)
		reactions[reactionType] = updated
	}
	return reactions
}

func isValidReaction(reactionType string) bool {
	return reactionType == "heart" || reactionType == "brokenheart"
}

func handleGetNews(c *Context) {
	c.json(200, Obj{"ok": true, "news": loadNews()})
}

func handleCreateNews(c *Context) {
	body := c.bodyJSON()
	title := strings.TrimSpace(toString(body["title"]))
	content := strings.TrimSpace(toString(body["body"]))
	if title == "" || len(title) > 120 {
		c.badRequest("invalid title")
		return
	}
	if content == "" || len(content) > 5000 {
		c.badRequest("invalid body")
		return
	}
	item := Obj{
		"id":        "nw" + randomString(12),
		"title":     title,
		"body":      content,
		"author":    c.getString("username"),
		"created":   timestamp(),
		"reactions": Obj{},
	}
	items := loadNews()
	next := []any{item}
	for _, it := range items {
		next = append(next, it)
	}
	saveNews(next)
	for _, f := range fsReadDir(usersDir) {
		if strings.HasSuffix(f, ".json") {
			addNotification(strings.TrimSuffix(f, ".json"), toString(item["author"]), "news", Obj{"newsId": item["id"], "title": item["title"]})
		}
	}
	c.json(201, Obj{"ok": true, "item": item})
}

func handleDeleteNews(c *Context) {
	id := c.param("id")
	items := loadNews()
	next := []any{}
	for _, it := range items {
		item := toObj(it)
		if toString(item["id"]) != id {
			next = append(next, item)
		}
	}
	saveNews(next)
	c.json(200, Obj{"ok": true})
}

func handleReactNews(c *Context) {
	body := c.bodyJSON()
	reactionType := toString(body["type"])
	if !isValidReaction(reactionType) {
		c.badRequest("invalid reaction")
		return
	}
	id := c.param("id")
	username := normalizeUsername(c.getString("username"))
	items := loadNews()
	found := false
	for _, it := range items {
		item := toObj(it)
		if toString(item["id"]) == id {
			reactions := Obj{}
			if item["reactions"] != nil {
				reactions = toObj(item["reactions"])
			}
			item["reactions"] = toggleReactionMap(reactions, username, reactionType)
			found = true
		}
	}
	if !found {
		c.notFound("news item not found")
		return
	}
	saveNews(items)
	c.json(200, Obj{"ok": true})
}
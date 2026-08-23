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

func isValidNewsCategory(category string) bool {
	return category == "update" || category == "release" || category == "event" || category == "poll" || category == "general"
}

func newsResponse(item Obj, username string) Obj {
	response := item
	if item["poll"] != nil {
		poll := toObj(item["poll"])
		options := asArray(poll["options"])
		total := float64(0)
		for i, opt := range options {
			option := toObj(opt)
			voters := asArray(option["voters"])
			option["votes"] = float64(len(voters))
			option["voted"] = containsStr(voters, username)
			option["voters"] = []any{}
			total += float64(len(voters))
			options[i] = option
		}
		poll["options"] = options
		poll["total"] = total
		response["poll"] = poll
	}
	return response
}

func handleGetNews(c *Context) {
	items := loadNews()
	for i, it := range items {
		items[i] = newsResponse(toObj(it), c.getString("username"))
	}
	c.json(200, Obj{"ok": true, "news": items})
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

	category := "update"
	if body["category"] != nil {
		category = strings.ToLower(strings.TrimSpace(toString(body["category"])))
	}
	if !isValidNewsCategory(category) {
		c.badRequest("invalid post type")
		return
	}

	link := Obj{}
	if body["linkUrl"] != nil && strings.TrimSpace(toString(body["linkUrl"])) != "" {
		linkUrl := strings.TrimSpace(toString(body["linkUrl"]))
		linkLabel := strings.TrimSpace(toString(body["linkLabel"]))
		if (!strings.HasPrefix(linkUrl, "https://") && !strings.HasPrefix(linkUrl, "/")) || linkLabel == "" || len(linkLabel) > 60 || len(linkUrl) > 500 {
			c.badRequest("invalid post link")
			return
		}
		link = Obj{"label": linkLabel, "url": linkUrl}
	}

	poll := Obj{}
	if category == "poll" {
		if body["options"] == nil {
			c.badRequest("polls need at least two options")
			return
		}
		rawOptions := asArray(body["options"])
		options := []any{}
		for _, it := range rawOptions {
			optionText := strings.TrimSpace(toString(it))
			if optionText != "" && len(optionText) <= 120 && len(options) < 6 {
				options = append(options, Obj{"id": "po" + randomString(8), "text": optionText, "voters": []any{}})
			}
		}
		if len(options) < 2 {
			c.badRequest("polls need at least two options")
			return
		}
		poll = Obj{"options": options}
	}

	item := Obj{
		"id":        "nw" + randomString(12),
		"title":     title,
		"body":      content,
		"category":  category,
		"author":    c.getString("username"),
		"created":   timestamp(),
		"reactions": Obj{},
	}
	if len(objKeys(link)) > 0 {
		item["link"] = link
	}
	if category == "poll" {
		item["poll"] = poll
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

func handleVoteNewsPoll(c *Context) {
	id := c.param("id")
	optionId := toString(c.bodyJSON()["option"])
	username := c.getString("username")
	items := loadNews()
	found := false
	validOption := false
	for i, it := range items {
		item := toObj(it)
		if toString(item["id"]) == id && item["poll"] != nil {
			found = true
			poll := toObj(item["poll"])
			options := asArray(poll["options"])
			alreadyVoted := false
			for _, opt := range options {
				option := toObj(opt)
				if toString(option["id"]) == optionId {
					validOption = true
					alreadyVoted = containsStr(asArray(option["voters"]), username)
				}
			}
			for p, opt := range options {
				option := toObj(opt)
				voters := removeStr(asArray(option["voters"]), username)
				if toString(option["id"]) == optionId && !alreadyVoted {
					voters = append(voters, username)
				}
				option["voters"] = voters
				options[p] = option
			}
			poll["options"] = options
			item["poll"] = poll
			items[i] = item
		}
	}
	if !found {
		c.notFound("poll not found")
		return
	}
	if !validOption {
		c.badRequest("invalid poll option")
		return
	}
	saveNews(items)
	c.json(200, Obj{"ok": true})
}
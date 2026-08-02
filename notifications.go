package main

// notifications.osl

func loadNotifications(username string) []any {
	key := normalizeUsername(username)
	if !isSafePathPart(key) {
		return []any{}
	}
	res := readFile(notificationsDir + key + ".json")
	if res.isErr() {
		return []any{}
	}
	data := res.data.(Obj)
	return asArray(data["items"])
}

func saveNotifications(username string, items []any) bool {
	key := normalizeUsername(username)
	if !isSafePathPart(key) {
		return false
	}
	return writeFile(notificationsDir+key+".json", Obj{"items": items})
}

func addNotification(target, actor, notifType string, extra Obj) {
	targetKey := normalizeUsername(target)
	if targetKey == "" || targetKey == normalizeUsername(actor) {
		return
	}
	item := Obj{
		"id":      "n" + randomString(12),
		"type":    notifType,
		"actor":   actor,
		"created": nowMs(),
		"read":    false,
	}
	for _, k := range objKeys(extra) {
		item[k] = extra[k]
	}
	extraKeys := objKeys(extra)
	items := loadNotifications(targetKey)
	next := []any{item}
	kept := 1
	for _, existingRaw := range items {
		existing := existingRaw.(map[string]any)
		same := toString(existing["type"]) == notifType &&
			normalizeUsername(toString(existing["actor"])) == normalizeUsername(actor)
		for _, k := range extraKeys {
			if !objHas(existing, k) || existing[k] != extra[k] {
				same = false
			}
		}
		if same && existing["read"] != true {
			return
		}
		if !same && kept < 50 {
			next = append(next, existingRaw)
			kept++
		}
	}
	saveNotifications(targetKey, next)
}

func handleGetNotifications(c *Context) {
	username := c.getString("username")
	c.json(200, Obj{"ok": true, "notifications": loadNotifications(username)})
}

func handleReadNotifications(c *Context) {
	username := c.getString("username")
	items := loadNotifications(username)
	for _, itemRaw := range items {
		item := itemRaw.(map[string]any)
		item["read"] = true
	}
	saveNotifications(username, items)
	c.json(200, Obj{"ok": true})
}
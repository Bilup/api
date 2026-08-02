package main

// settings.go — maps settings.osl.

func settingsPath(username string) string {
	return userSettingsDir + normalizeUsername(username) + ".json"
}

func handleGetMySettings(c *Context) {
	username := c.getString("username")
	key := normalizeUsername(username)
	if !isSafePathPart(key) {
		c.badRequest("invalid username")
		return
	}
	res := readFile(settingsPath(username))
	if !res.isOk() {
		c.json(200, Obj{"ok": true, "settings": Obj{}})
		return
	}
	c.json(200, Obj{"ok": true, "settings": res.unwrap()})
}

func handlePutMySettings(c *Context) {
	username := c.getString("username")
	key := normalizeUsername(username)
	if !isSafePathPart(key) {
		c.badRequest("invalid username")
		return
	}
	body := string(c.readBody())
	if len(body) > 131072 {
		c.badRequest("settings too large")
		return
	}
	parsed := tryParseJSON(body, "object")
	if parsed.isErr() {
		c.badRequest("settings must be a JSON object")
		return
	}
	settings := toObj(parsed.unwrap())
	if settings["settings"] == nil {
		settings["settings"] = Obj{}
	}
	inner := toObj(settings["settings"])
	inner["updatedAt"] = timestamp()
	// write back the modified inner object
	settings["settings"] = inner
	if !writeFile(settingsPath(username), settings) {
		c.internalError("failed to save settings")
		return
	}
	c.json(200, Obj{"ok": true, "updatedAt": inner["updatedAt"]})
}
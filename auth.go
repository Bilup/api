package main

import (
	"net/url"
	"strings"
)

// auth.osl

func setAuthCookie(c *Context, sessionId string) {
	secure := strings.HasPrefix(appURL, "https://")
	c.setCookie("auth_token", sessionId, 604800, "/", "", secure, true)
}
func clearAuthCookie(c *Context) { c.clearCookie("auth_token") }

func createSession(username, userId string) string {
	sessionId := randomString(32)
	session := Obj{"username": username, "userId": userId, "createdAt": nowMs()}
	writeFile(sessionsDir+sessionId+".json", session)
	sessionCache.set(sessionId, session)
	return sessionId
}

func getSession(token string) Obj {
	if !isSafePathPart(token) {
		return Obj{"ok": false, "error": "invalid token"}
	}
	path := sessionsDir + token + ".json"
	var session Obj
	if v := sessionCache.get(token); v != nil {
		session = v.(Obj)
	} else {
		res := readFile(path)
		if res.isErr() {
			return Obj{"ok": false, "error": "session not found"}
		}
		session = res.data.(Obj)
		sessionCache.set(token, session)
	}
	if float64(nowMs())-toFloat(session["createdAt"]) > 604800000 {
		fsRemove(path)
		sessionCache.delete(token)
		return Obj{"ok": false, "error": "session expired"}
	}
	username := toString(session["username"])
	userId := toString(session["userId"])
	if mapped := usernameForId(userId); mapped != "" {
		username = mapped
	}
	return Obj{"ok": true, "username": username, "userId": userId}
}

func deleteSession(token string) bool {
	if !isSafePathPart(token) {
		return false
	}
	sessionCache.delete(token)
	return fsRemove(sessionsDir + token + ".json")
}

func cleanupExpiredSessions() {
	for _, name := range fsReadDir(sessionsDir) {
		path := sessionsDir + name
		res := readFile(path)
		if res.isOk() {
			session := res.data.(Obj)
			if float64(nowMs())-toFloat(session["createdAt"]) > 604800000 {
				fsRemove(path)
			}
		}
	}
}

func requestToken(c *Context) string {
	if t := c.bearer(); t != "" {
		return t
	}
	return c.cookie("auth_token")
}

func isAdminUser(username string) bool {
	key := normalizeUsername(username)
	for _, a := range adminUsers {
		if a == key {
			return true
		}
	}
	for _, a := range cachedAdmins() {
		if normalizeUsername(toString(a)) == key {
			return true
		}
	}
	return false
}

func handleRoturAuth(c *Context) {
	validator := c.query("v", "")
	if validator == "" {
		c.json(401, Obj{"ok": false, "error": "missing validator"})
		return
	}
	resp := requestsGet("https://api.accounts.bilup.org/validate?key="+roturAppKey+"&v="+url.QueryEscape(validator), nil)
	if !resp.success {
		c.json(401, Obj{"ok": false, "error": "failed to validate token"})
		return
	}
	parsed := tryParseJSON(resp.body, "object")
	if parsed.isErr() {
		c.json(401, Obj{"ok": false, "error": "invalid response from rotur"})
		return
	}
	data := parsed.data.(Obj)
	if objHas(data, "error") && data["error"] != nil {
		c.json(401, Obj{"ok": false, "error": "rotur: " + toString(data["error"])})
		return
	}
	if !objHas(data, "valid") || data["valid"] != true {
		c.json(401, Obj{"ok": false, "error": "rotur rejected the login validator"})
		return
	}
	userId := toString(data["id"])
	username := toString(data["username"])
	ensureUserIdentity(userId, username)

	if isBannedUser(username) {
		reason := banReasonFor(username)
		banErr := "This account is banned from Bilup."
		if reason != "" {
			banErr = "This account is banned from Bilup: " + reason
		}
		c.json(403, Obj{"ok": false, "code": "banned", "error": banErr})
		return
	}

	profile := loadProfile(username)
	profile["username"] = username
	saveProfile(profile)

	sessionId := createSession(username, userId)
	setAuthCookie(c, sessionId)

	c.json(200, Obj{
		"ok":      true,
		"token":   sessionId,
		"username": username,
		"isAdmin": isAdminUser(username),
	})
}

func handleLogout(c *Context) {
	token := requestToken(c)
	if token != "" {
		deleteSession(token)
	}
	clearAuthCookie(c)
	c.json(200, Obj{"ok": true})
}

func handleGetMe(c *Context) {
	if !c.getBool("authenticated") {
		c.json(401, Obj{"ok": false, "error": "not authenticated"})
		return
	}
	username := c.getString("username")
	profile := loadProfile(username)
	c.json(200, Obj{
		"ok":              true,
		"username":        username,
		"bio":             profile["bio"],
		"featuredProject": profile["featuredProject"],
		"isAdmin":         c.getBool("isAdmin"),
		"standing":        standingLevel(username),
	})
}

func handleHealth(c *Context) { c.json(200, Obj{"ok": true}) }
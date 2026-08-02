package main

// middleware.osl

func corsMiddleware(c *Context) {
	origin := c.headerVal("Origin")
	if origin != "" {
		c.setHeader("Access-Control-Allow-Origin", origin)
		if arrayContains(corsAllowedOrigins, origin) {
			c.setHeader("Access-Control-Allow-Credentials", "true")
		}
		c.setHeader("Vary", "Origin")
	}
	c.setHeader("Access-Control-Allow-Methods", "GET, POST, PUT, DELETE, OPTIONS")
	c.setHeader("Access-Control-Allow-Headers", "Content-Type, Authorization")
	c.setHeader("Access-Control-Max-Age", "86400")

	if c.method() == "OPTIONS" {
		c.noContent()
		return
	}
	c.next()
}

func authMiddleware(c *Context) {
	authenticated := false
	username := ""
	isAdmin := false

	token := requestToken(c)
	if token != "" {
		session := getSession(token)
		if session["ok"] == true && !isBannedUser(toString(session["username"])) {
			authenticated = true
			username = toString(session["username"])
			isAdmin = isAdminUser(username)
		}
	}
	c.set("authenticated", authenticated)
	c.set("username", username)
	c.set("isAdmin", isAdmin)
	c.next()
}

func requireAuth(c *Context) {
	if !c.getBool("authenticated") {
		c.json(401, Obj{"ok": false, "error": "not authenticated"})
		c.abort()
		return
	}
	c.next()
}

func requireAdmin(c *Context) {
	if !c.getBool("authenticated") {
		c.json(401, Obj{"ok": false, "error": "not authenticated"})
		c.abort()
		return
	}
	if !c.getBool("isAdmin") {
		c.json(403, Obj{"ok": false, "error": "admin access required"})
		c.abort()
		return
	}
	c.next()
}

func arrayContains(list []string, s string) bool {
	for _, v := range list {
		if v == s {
			return true
		}
	}
	return false
}
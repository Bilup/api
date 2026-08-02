package main

import "strings"

// standing.go — maps standing.osl: account membership standing levels +
// admin moderation middleware/handlers.

func standingRank(level string) float64 {
	switch level {
	case "warning":
		return 1
	case "suspended":
		return 2
	case "banned":
		return 3
	}
	return 0
}

func standingRecoverMs(level string) float64 {
	switch level {
	case "warning":
		return 604800000
	case "suspended":
		return 2592000000
	}
	return 0
}

func nextRecoveryLevel(level string) string {
	if level == "suspended" || level == "warning" {
		return "good"
	}
	return level
}

func applyStandingRecovery(profile Obj) bool {
	if profile["standing"] == nil {
		return false
	}
	level := toString(profile["standing"])
	recoverAt := float64(0)
	if profile["standingRecoverAt"] != nil {
		recoverAt = toFloat(profile["standingRecoverAt"])
	}
	changed := false
	for recoverAt > 0 && float64(timestamp()) > recoverAt && level != "good" && level != "banned" {
		level = nextRecoveryLevel(level)
		ms := standingRecoverMs(level)
		if ms > 0 {
			recoverAt = float64(timestamp()) + ms
		} else {
			recoverAt = 0
		}
		changed = true
	}
	if changed {
		profile["standing"] = level
		profile["standingRecoverAt"] = recoverAt
	}
	return changed
}

func standingLevel(username string) string {
	if isBannedUser(username) {
		return "banned"
	}
	profile := loadProfile(username)
	if applyStandingRecovery(profile) {
		saveProfile(profile)
	}
	if profile["standing"] == nil {
		return "good"
	}
	return toString(profile["standing"])
}

func standingInfo(username string) Obj {
	profile := loadProfile(username)
	if applyStandingRecovery(profile) {
		saveProfile(profile)
	}
	level := "good"
	if profile["standing"] != nil {
		level = toString(profile["standing"])
	}
	if level != "banned" && isBannedUser(username) {
		level = "banned"
	}
	recoverAt := float64(0)
	if profile["standingRecoverAt"] != nil {
		recoverAt = toFloat(profile["standingRecoverAt"])
	}
	history := []any{}
	if profile["standingHistory"] != nil {
		history = asArray(profile["standingHistory"])
	}
	return Obj{"level": level, "recoverAt": recoverAt, "history": history}
}

func setStanding(username, level, reason, actor string) bool {
	if level != "good" && level != "warning" && level != "suspended" && level != "banned" {
		return false
	}
	profile := loadProfile(username)
	prev := "good"
	if profile["standing"] != nil {
		prev = toString(profile["standing"])
	}
	profile["standing"] = level
	ms := standingRecoverMs(level)
	if ms > 0 {
		profile["standingRecoverAt"] = float64(timestamp()) + ms
	} else {
		profile["standingRecoverAt"] = float64(0)
	}
	history := []any{}
	if profile["standingHistory"] != nil {
		history = asArray(profile["standingHistory"])
	}
	entry := Obj{"level": level, "previous": prev, "reason": reason, "by": actor, "created": timestamp()}
	nextHistory := []any{entry}
	kept := 1
	for _, it := range history {
		if kept < 20 {
			nextHistory = append(nextHistory, it)
			kept++
		}
	}
	profile["standingHistory"] = nextHistory
	saveProfile(profile)
	if level == "banned" {
		banUser(username, reason, actor)
	} else if isBannedUser(username) {
		unbanUser(username)
	}
	addNotification(username, actor, "standing", Obj{"level": level, "reason": reason})
	return true
}

func requireGoodStanding(c *Context) {
	level := standingLevel(c.getString("username"))
	if standingRank(level) >= 2 {
		c.json(403, Obj{"ok": false, "error": "Your account is " + level + " and cannot do this right now.", "standing": level})
		c.abort()
		return
	}
	c.next()
}

func handleSetStanding(c *Context) {
	body := c.bodyJSON()
	name := toString(body["username"])
	level := toString(body["level"])
	reason := ""
	if body["reason"] != nil {
		reason = strings.TrimSpace(toString(body["reason"]))
	}
	if normalizeUsername(name) == "" {
		c.badRequest("username is required")
		return
	}
	if isAdminUser(name) && level != "good" {
		c.badRequest("cannot moderate an admin")
		return
	}
	if !setStanding(name, level, reason, c.getString("username")) {
		c.badRequest("invalid standing level")
		return
	}
	c.json(200, Obj{"ok": true, "standing": standingInfo(name)})
}

func handleMessageUser(c *Context) {
	body := c.bodyJSON()
	name := toString(body["username"])
	message := strings.TrimSpace(toString(body["message"]))
	if normalizeUsername(name) == "" || message == "" || len(message) > 1000 {
		c.badRequest("username and message are required")
		return
	}
	addNotification(name, c.getString("username"), "moderation", Obj{"message": message})
	c.json(200, Obj{"ok": true})
}

func handleGetUserAdmin(c *Context) {
	name := c.query("username", "")
	if normalizeUsername(name) == "" {
		c.badRequest("username is required")
		return
	}
	profile := loadProfile(name)
	commentsOff := profile["commentsOff"] == true
	followers := asArray(profile["followers"])
	following := asArray(profile["following"])
	c.json(200, Obj{
		"ok":              true,
		"username":        profile["username"],
		"featuredProject": profile["featuredProject"],
		"commentsOff":     commentsOff,
		"followerCount":   float64(len(followers)),
		"followingCount":  float64(len(following)),
		"created":         profile["created"],
		"standing":        standingInfo(name),
		"banned":          isBannedUser(name),
		"admin":           isAdminUser(name),
		"projects":        ownerIndexEntries(name, true),
		"quota": Obj{
			"used":  weeklyUploadedBytes(name),
			"limit": weeklyUploadQuotaBytes,
		},
	})
}

func handleAdminUpdateProfile(c *Context) {
	body := c.bodyJSON()
	name := toString(body["username"])
	if normalizeUsername(name) == "" {
		c.badRequest("username is required")
		return
	}
	profile := loadProfile(name)
	if body["featuredProject"] != nil {
		profile["featuredProject"] = toString(body["featuredProject"])
	}
	if body["commentsOff"] != nil {
		profile["commentsOff"] = toBool(body["commentsOff"])
	}
	saveProfile(profile)
	c.json(200, Obj{"ok": true})
}
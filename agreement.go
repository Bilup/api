package main

import "strings"

// agreement.osl — service agreement meta/text and user acceptances.

func agreementMetaPath() string { return "data/agreement.json" }
func agreementTextPath() string { return "data/agreement.md" }

func loadAgreement() Obj {
	meta := Obj{"text": "", "version": float64(0), "updatedAt": float64(0)}
	res := readFile(agreementMetaPath())
	if res.isOk() {
		meta = toObj(res.unwrap())
	}
	if fsExists(agreementTextPath()) {
		meta["text"] = fsReadFile(agreementTextPath())
	}
	return meta
}

func saveAgreement(data Obj) bool {
	text := toString(data["text"])
	data["text"] = ""
	ok := writeFile(agreementMetaPath(), data)
	data["text"] = text
	if text != "" {
		if !fsWriteFile(agreementTextPath(), text) {
			ok = false
		}
	}
	return ok
}

func acceptancePath(username string) string {
	return agreementAcceptDir + normalizeUsername(username) + ".json"
}

func userHasAcceptedVersion(username string, version float64) bool {
	key := normalizeUsername(username)
	if !isSafePathPart(key) {
		return false
	}
	res := readFile(acceptancePath(username))
	if !res.isOk() {
		return false
	}
	data := toObj(res.unwrap())
	return toFloat(data["version"]) == version
}

func handleGetAgreement(c *Context) {
	agreement := loadAgreement()
	respAgreement := Obj{
		"text":      agreement["text"],
		"version":   agreement["version"],
		"updatedAt": agreement["updatedAt"],
	}
	if c.getBool("authenticated") {
		respAgreement["accepted"] = userHasAcceptedVersion(c.getString("username"), toFloat(agreement["version"]))
	}
	c.json(200, Obj{"ok": true, "agreement": respAgreement})
}

func handleAcceptAgreement(c *Context) {
	username := c.getString("username")
	agreement := loadAgreement()
	version := toFloat(agreement["version"])

	if version == 0 {
		c.badRequest("no agreement to accept")
		return
	}
	if userHasAcceptedVersion(username, version) {
		c.json(200, Obj{"ok": true, "already": true})
		return
	}

	acceptance := Obj{
		"version":    version,
		"acceptedAt": timestamp(),
		"username":   username,
	}
	key := normalizeUsername(username)
	if !isSafePathPart(key) {
		c.badRequest("invalid username")
		return
	}
	fsMkdirAll(agreementAcceptDir)
	if !writeFile(acceptancePath(username), acceptance) {
		c.internalError("failed to save acceptance")
		return
	}
	c.json(200, Obj{"ok": true})
}

func handleAdminGetAgreement(c *Context) {
	agreement := loadAgreement()
	version := toFloat(agreement["version"])
	acceptedCount := float64(0)
	totalUsers := float64(0)

	fsMkdirAll(agreementAcceptDir)
	for _, name := range fsReadDir(agreementAcceptDir) {
		if strings.HasSuffix(name, ".json") {
			totalUsers++
			res := readFile(agreementAcceptDir + name)
			if res.isOk() {
				acc := toObj(res.unwrap())
				if toFloat(acc["version"]) == version {
					acceptedCount++
				}
			}
		}
	}

	c.json(200, Obj{
		"ok":               true,
		"agreement":        agreement,
		"acceptedCount":    acceptedCount,
		"totalAcceptances": totalUsers,
	})
}

func handleAdminUpdateAgreement(c *Context) {
	body := c.bodyJSON()
	text := strings.TrimSpace(toString(body["text"]))
	if text == "" {
		c.badRequest("agreement text cannot be empty")
		return
	}
	agreement := loadAgreement()
	agreement["text"] = text
	agreement["version"] = toFloat(agreement["version"]) + 1
	agreement["updatedAt"] = timestamp()
	if !saveAgreement(agreement) {
		c.internalError("failed to save agreement")
		return
	}
	c.json(200, Obj{"ok": true, "version": agreement["version"]})
}
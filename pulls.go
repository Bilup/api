package main

import "strings"

// pulls.go - maps pulls.osl.

func loadViewableProject(c *Context) Obj {
	res := loadProject(c.param("id"))
	if !res.isOk() {
		return Obj{"ok": false}
	}
	project := toObj(res.unwrap())
	if !canViewProject(project, c) {
		return Obj{"ok": false}
	}
	if projectPrice(project) > 0 && !canAccessPaidJson(project, c) {
		return Obj{"ok": false}
	}
	return Obj{"ok": true, "project": project}
}

func projectHasRepo(project Obj) bool {
	return project["repo"] != nil
}

func commitAuthorName(commit Obj) string {
	if commit["commit"] == nil {
		return ""
	}
	inner := toObj(commit["commit"])
	if inner["author"] == nil {
		return ""
	}
	return toString(toObj(inner["author"])["name"])
}

func handleGetCommits(c *Context) {
	viewable := loadViewableProject(c)
	if !toBool(viewable["ok"]) {
		c.notFound("project not found")
		return
	}
	project := toObj(viewable["project"])
	repo := toObj(project["repo"])

	resp := giteaGet(repoApiPath(project)+"/commits?limit=50&stat=false&verification=false&sha="+urlEscape(toString(repo["defaultBranch"])), "")
	parsed := parseGiteaArray(resp)
	if !toBool(parsed["ok"]) {
		c.internalError("failed to load commits")
		return
	}
	commits := asArray(parsed["data"])
	slim := []any{}
	for _, it := range commits {
		commit := toObj(it)
		inner := toObj(commit["commit"])
		author := toObj(inner["author"])
		slim = append(slim, Obj{
			"sha":     commit["sha"],
			"message": inner["message"],
			"author":  commitAuthorName(commit),
			"date":    author["date"],
		})
	}
	c.json(200, Obj{"ok": true, "commits": slim})
}

func slimPull(pull Obj) Obj {
	user := toObj(pull["user"])
	head := toObj(pull["head"])
	base := toObj(pull["base"])
	headOwner := ""
	if head["repo"] != nil {
		headRepo := toObj(head["repo"])
		headOwner = toString(toObj(headRepo["owner"])["login"])
	}
	return Obj{
		"index":      pull["number"],
		"title":      pull["title"],
		"body":       pull["body"],
		"state":      pull["state"],
		"user":       user["login"],
		"headOwner":  headOwner,
		"headBranch": head["ref"],
		"baseBranch": base["ref"],
		"created":    pull["created_at"],
		"merged":     pull["merged"],
		"mergeable":  pull["mergeable"],
	}
}

func handleListPulls(c *Context) {
	viewable := loadViewableProject(c)
	if !toBool(viewable["ok"]) {
		c.notFound("project not found")
		return
	}
	project := toObj(viewable["project"])
	resp := giteaGet(repoApiPath(project)+"/pulls?state=all&limit=50", "")
	parsed := parseGiteaArray(resp)
	if !toBool(parsed["ok"]) {
		c.internalError("failed to load pull requests")
		return
	}
	slim := []any{}
	for _, it := range asArray(parsed["data"]) {
		slim = append(slim, slimPull(toObj(it)))
	}
	c.json(200, Obj{"ok": true, "pulls": slim})
}

func handleCreatePull(c *Context) {
	viewable := loadViewableProject(c)
	if !toBool(viewable["ok"]) {
		c.notFound("project not found")
		return
	}
	project := toObj(viewable["project"])
	repo := toObj(project["repo"])
	body := c.bodyJSON()

	title := strings.TrimSpace(toString(body["title"]))
	if title == "" || len(title) > 200 {
		c.badRequest("invalid title")
		return
	}
	headOwner := toString(body["headOwner"])
	headBranch := toString(body["headBranch"])
	if headOwner == "" || headBranch == "" {
		c.badRequest("missing head")
		return
	}

	description := ""
	if body["body"] != nil {
		description = toString(body["body"])
	}

	head := headOwner + ":" + headBranch
	if normalizeUsername(headOwner) == normalizeUsername(toString(repo["owner"])) {
		head = headBranch
	}

	resp := giteaPost(repoApiPath(project)+"/pulls", Obj{
		"title": title,
		"body":  description,
		"head":  head,
		"base":  repo["defaultBranch"],
	}, c.getString("username"))
	parsed := parseGiteaObject(resp)
	if !toBool(parsed["ok"]) {
		c.json(502, Obj{"ok": false, "error": "failed to create pull request"})
		return
	}
	c.json(201, Obj{"ok": true, "pull": slimPull(toObj(parsed["data"]))})
}

func handleGetPull(c *Context) {
	viewable := loadViewableProject(c)
	if !toBool(viewable["ok"]) {
		c.notFound("project not found")
		return
	}
	project := toObj(viewable["project"])
	resp := giteaGet(repoApiPath(project)+"/pulls/"+c.param("index"), "")
	parsed := parseGiteaObject(resp)
	if !toBool(parsed["ok"]) {
		c.notFound("pull request not found")
		return
	}
	c.json(200, Obj{"ok": true, "pull": slimPull(toObj(parsed["data"]))})
}

func handleGetPullDiff(c *Context) {
	viewable := loadViewableProject(c)
	if !toBool(viewable["ok"]) {
		c.notFound("project not found")
		return
	}
	project := toObj(viewable["project"])
	resp := requestsGet(giteaURL+"/api/v1"+repoApiPath(project)+"/pulls/"+c.param("index")+".diff", map[string]any{"headers": giteaHeaders("")})
	if !resp.success {
		c.notFound("diff not found")
		return
	}
	c.text(200, resp.body)
}

func handleMergePull(c *Context) {
	res := loadProject(c.param("id"))
	if !res.isOk() {
		c.notFound("project not found")
		return
	}
	project := toObj(res.unwrap())
	if !canEditProject(project, c) {
		c.forbidden("only the project owner can merge")
		return
	}
	index := c.param("index")
	pullResp := giteaGet(repoApiPath(project)+"/pulls/"+index, "")
	parsed := parseGiteaObject(pullResp)
	if !toBool(parsed["ok"]) {
		c.notFound("pull request not found")
		return
	}
	pull := toObj(parsed["data"])
	if pull["mergeable"] != true {
		c.json(409, Obj{"ok": false, "error": "merge conflict", "code": "conflict"})
		return
	}
	resp := giteaPost(repoApiPath(project)+"/pulls/"+index+"/merge", Obj{"Do": "merge"}, c.getString("username"))
	if !resp.success {
		c.json(502, Obj{"ok": false, "error": "merge failed"})
		return
	}
	c.json(200, Obj{"ok": true})
}
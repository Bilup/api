package main

// gitea.osl

func giteaHeaders(sudoUser string) map[string]any {
	headers := map[string]any{
		"Authorization": "token " + giteaAdminToken,
		"Content-Type":  "application/json",
	}
	if sudoUser != "" {
		headers["Sudo"] = sudoUser
	}
	return headers
}

func giteaGet(path, sudoUser string) httpResp {
	return requestsGet(giteaURL+"/api/v1"+path, map[string]any{"headers": giteaHeaders(sudoUser)})
}

func giteaPost(path string, body Obj, sudoUser string) httpResp {
	return requestsPost(giteaURL+"/api/v1"+path, map[string]any{
		"headers": giteaHeaders(sudoUser),
		"body":    jsonString(body),
	})
}

func parseGiteaObject(resp httpResp) Obj {
	if !resp.success {
		return Obj{"ok": false, "error": "gitea request failed", "status": resp.status}
	}
	parsed := tryParseJSON(resp.body, "object")
	if parsed.isErr() {
		return Obj{"ok": false, "error": "invalid gitea response"}
	}
	return Obj{"ok": true, "data": parsed.data}
}

func parseGiteaArray(resp httpResp) Obj {
	if !resp.success {
		return Obj{"ok": false, "error": "gitea request failed", "status": resp.status}
	}
	parsed := tryParseJSON(resp.body, "array")
	if parsed.isErr() {
		return Obj{"ok": false, "error": "invalid gitea response"}
	}
	return Obj{"ok": true, "data": parsed.data}
}

func repoApiPath(project Obj) string {
	repo := project["repo"].(map[string]any)
	return "/repos/" + toString(repo["owner"]) + "/" + toString(repo["name"])
}
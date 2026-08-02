package main

import "strings"

// reports.go — maps reports.osl.

func loadReports() []any {
	res := readFile(reportsFile)
	if !res.isOk() {
		return []any{}
	}
	data := toObj(res.unwrap())
	return asArray(data["reports"])
}

func saveReports(reports []any) bool {
	return writeFile(reportsFile, Obj{"reports": reports})
}

func handleCreateReport(c *Context) {
	body := c.bodyJSON()
	reportType := toString(body["type"])
	target := toString(body["target"])
	reason := strings.TrimSpace(toString(body["reason"]))

	if reportType != "project" && reportType != "user" && reportType != "comment" {
		c.badRequest("invalid report type")
		return
	}
	if target == "" || reason == "" || len(reason) > 1000 {
		c.badRequest("invalid report")
		return
	}

	report := Obj{
		"id":       "r" + randomString(12),
		"reporter": c.getString("username"),
		"type":     reportType,
		"target":   target,
		"reason":   reason,
		"created":  timestamp(),
		"resolved": false,
	}
	if body["context"] != nil {
		report["context"] = toString(body["context"])
	}
	reports := loadReports()
	reports = append(reports, report)
	saveReports(reports)
	if reportType == "project" {
		snapshotProjectEvidence(target)
	}
	c.json(201, Obj{"ok": true})
}

func handleGetReports(c *Context) {
	c.json(200, Obj{"ok": true, "reports": loadReports()})
}

func handleResolveReport(c *Context) {
	body := c.bodyJSON()
	id := toString(body["id"])
	reports := loadReports()
	found := false
	projectTarget := ""
	for _, it := range reports {
		report := toObj(it)
		if toString(report["id"]) == id {
			report["resolved"] = true
			found = true
			if toString(report["type"]) == "project" {
				projectTarget = toString(report["target"])
			}
		}
	}
	if !found {
		c.notFound("report not found")
		return
	}
	saveReports(reports)
	if projectTarget != "" {
		maybeClearProjectEvidence(projectTarget)
	}
	c.json(200, Obj{"ok": true})
}
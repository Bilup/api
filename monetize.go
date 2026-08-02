package main

import "strconv"

// monetize.osl — helper functions used across the platform root (partially ported).

func projectVisibility(project Obj) string {
	if v, ok := project["visibility"]; ok && v != nil {
		s := toString(v)
		if s == "public" || s == "unlisted" || s == "private" {
			return s
		}
	}
	if project["shared"] == true {
		return "public"
	}
	return "private"
}

func projectIsViewable(project Obj) bool { return projectVisibility(project) != "private" }

func projectPrice(project Obj) float64 {
	if v, ok := project["price"]; ok && v != nil {
		p := toFloat(v)
		if p > 0 {
			return p
		}
	}
	return 0
}

func projectRevenue(project Obj) float64 {
	if v, ok := project["revenue"]; ok && v != nil {
		return toFloat(v)
	}
	return 0
}

func projectSaves(project Obj) float64 {
	if v, ok := project["saves"]; ok && v != nil {
		return toFloat(v)
	}
	return 0
}

func projectBuyers(project Obj) []any { return asArray(project["buyers"]) }

func hasBought(project Obj, userLower string) bool {
	if userLower == "" {
		return false
	}
	for _, entry := range projectBuyers(project) {
		e := entry.(map[string]any)
		if normalizeUsername(toString(e["user"])) == userLower {
			return true
		}
	}
	return false
}

func canAccessPaidJson(project Obj, c *Context) bool {
	if canEditProject(project, c) {
		return true
	}
	return hasBought(project, normalizeUsername(c.getString("username")))
}

func canInteractProject(project Obj, c *Context) bool {
	if projectPrice(project) > 0 {
		return canAccessPaidJson(project, c)
	}
	return true
}

func projectRemixable(project Obj) bool {
	if v, ok := project["remixable"]; ok && v == false {
		return false
	}
	return true
}

func projectSeeInside(project Obj) bool {
	if v, ok := project["seeInside"]; ok && v == false {
		return false
	}
	return true
}

func canSeeInsideProject(project Obj, c *Context) bool {
	if canEditProject(project, c) {
		return true
	}
	if !projectSeeInside(project) {
		return false
	}
	if !projectIsViewable(project) {
		return false
	}
	if projectPrice(project) > 0 {
		return canAccessPaidJson(project, c)
	}
	return true
}

func canRemixProject(project Obj, c *Context) bool {
	if !projectRemixable(project) {
		return canEditProject(project, c)
	}
	return canSeeInsideProject(project, c)
}

func roturGet(path string) httpResp {
	return requestsGet(roturBase+path, Obj{"headers": Obj{"Authorization": "Bearer " + bilupRoturToken}})
}

func roturPost(path string, body Obj) httpResp {
	return requestsPost(roturBase+path, Obj{
		"headers": Obj{"Authorization": "Bearer " + bilupRoturToken, "Content-Type": "application/json"},
		"body":    jsonString(body),
	})
}

func payoutTo(recipient string, amount float64, note string) bool {
	payResp := roturPost("/me/transfer", Obj{"to": recipient, "amount": amount, "note": note})
	return payResp.success && payResp.status == 200
}

func bilupAccountUser() string {
	if bilupRoturUser != "" {
		return bilupRoturUser
	}
	resp := roturGet("/me")
	if !resp.success || resp.status != 200 {
		return ""
	}
	parsed := tryParseJSON(resp.body, "object")
	if parsed.isErr() {
		return ""
	}
	me := toObj(parsed.unwrap())
	if me["username"] != nil {
		return toString(me["username"])
	}
	return ""
}

func verifyIncomingTransfer(fromLower, note string, minAmount float64) bool {
	resp := roturGet("/me")
	if !resp.success || resp.status != 200 {
		return false
	}
	parsed := tryParseJSON(resp.body, "object")
	if parsed.isErr() {
		return false
	}
	me := toObj(parsed.unwrap())
	if me["sys.transactions"] == nil {
		return false
	}
	for _, it := range asArray(me["sys.transactions"]) {
		tx := toObj(it)
		if toString(tx["type"]) == "in" {
			if toString(tx["note"]) == note && normalizeUsername(toString(tx["user"])) == fromLower && toFloat(tx["amount"])+0.001 >= minAmount {
				return true
			}
		}
	}
	return false
}

func loadPurchaseKeys() Obj {
	res := readFile(purchaseKeysFile)
	if !res.isOk() {
		return Obj{"keys": Obj{}}
	}
	return toObj(res.unwrap())
}

func savePurchaseKeys(store Obj) bool { return writeFile(purchaseKeysFile, store) }

func loadPendingPayouts() Obj {
	res := readFile(pendingPayoutsFile)
	if !res.isOk() {
		return Obj{"payouts": []any{}}
	}
	return toObj(res.unwrap())
}

func recordPendingPayout(projectId, seller, buyer string, amount float64) {
	store := loadPendingPayouts()
	payouts := asArray(store["payouts"])
	payouts = append(payouts, Obj{"projectId": projectId, "seller": seller, "buyer": buyer, "amount": amount, "at": timestamp()})
	store["payouts"] = payouts
	writeFile(pendingPayoutsFile, store)
}

func retryPendingPayouts() (paid, remaining float64) {
	store := loadPendingPayouts()
	payouts := asArray(store["payouts"])
	rest := []any{}
	paidCount := float64(0)
	for _, it := range payouts {
		p := toObj(it)
		if payoutTo(toString(p["seller"]), toFloat(p["amount"]), "Bilup sale (retry)") {
			paidCount++
		} else {
			rest = append(rest, p)
		}
	}
	store["payouts"] = rest
	writeFile(pendingPayoutsFile, store)
	return paidCount, float64(len(rest))
}

func handleListPendingPayouts(c *Context) {
	store := loadPendingPayouts()
	c.json(200, Obj{"ok": true, "payouts": asArray(store["payouts"])})
}

func handleRetryPendingPayouts(c *Context) {
	if bilupRoturToken == "" {
		c.json(503, Obj{"ok": false, "error": "payouts are not available right now"})
		return
	}
	paid, remaining := retryPendingPayouts()
	c.json(200, Obj{"ok": true, "paid": paid, "remaining": remaining})
}

func prunePurchaseKeys(store Obj) Obj {
	keys := toObj(store["keys"])
	kept := Obj{}
	for _, k := range objKeys(keys) {
		entry := toObj(keys[k])
		if float64(timestamp())-toFloat(entry["at"]) < 3600000 {
			kept[k] = entry
		}
	}
	store["keys"] = kept
	return store
}

func mergeDayHistory(into, from Obj) {
	for _, k := range objKeys(from) {
		cur := float64(0)
		if into[k] != nil {
			cur = toFloat(into[k])
		}
		into[k] = cur + toFloat(from[k])
	}
}

func todayIndex() string { return strconv.Itoa(int(timestamp() / 86400000)) }

func handleGetMyStats(c *Context) {
	owner := normalizeUsername(c.getString("username"))
	index := loadIndex()
	projects := toObj(index["projects"])
	keys := objKeys(projects)
	today := int(timestamp() / 86400000)

	var totalViews, totalHearts, totalRevenue, totalBuyers, projectCount, sharedCount float64
	viewHistory := Obj{}
	saleHistory := Obj{}

	for _, k := range keys {
		entry := toObj(projects[k])
		if entry["owner"] == nil || normalizeUsername(toString(entry["owner"])) != owner {
			continue
		}
		res := loadProject(toString(entry["id"]))
		if !res.isOk() {
			continue
		}
		p := toObj(res.unwrap())
		projectCount++
		if projectVisibility(p) == "public" {
			sharedCount++
		}
		totalViews += toFloat(p["views"])
		totalHearts += float64(len(asArray(p["loves"])))
		totalRevenue += projectRevenue(p)
		totalBuyers += float64(len(projectBuyers(p)))
		if p["viewHistory"] != nil {
			mergeDayHistory(viewHistory, toObj(p["viewHistory"]))
		}
		if p["saleHistory"] != nil {
			mergeDayHistory(saleHistory, toObj(p["saleHistory"]))
		}
	}

	viewsThisMonth := float64(0)
	for _, k := range objKeys(viewHistory) {
		if float64(today)-toFloat(k) < 30 {
			viewsThisMonth += toFloat(viewHistory[k])
		}
	}

	c.json(200, Obj{"ok": true, "stats": Obj{
		"totalViews":    totalViews,
		"viewsThisMonth": viewsThisMonth,
		"totalHearts":    totalHearts,
		"totalRevenue":   totalRevenue,
		"totalBuyers":    totalBuyers,
		"projectCount":   projectCount,
		"sharedCount":    sharedCount,
		"viewHistory":    viewHistory,
		"saleHistory":    saleHistory,
	}})
}

func handleSetVisibility(c *Context) {
	res := loadProject(c.param("id"))
	if !res.isOk() {
		c.notFound("project not found")
		return
	}
	project := toObj(res.unwrap())
	if !canEditProject(project, c) {
		c.forbidden("not your project")
		return
	}
	body := c.bodyJSON()
	target := toString(body["visibility"])
	if target != "public" && target != "unlisted" && target != "private" {
		c.badRequest("visibility must be public, unlisted, or private")
		return
	}

	firstShare := false
	if target == "public" || target == "unlisted" {
		blockedExtension := projectBlockedExtension(project)
		if blockedExtension != "" {
			c.badRequest("remove the blocked extension before sharing: " + blockedExtension)
			return
		}
		everUploaded := toFloat(project["lastUploadAt"]) > 0
		everUploaded = everUploaded || toBool(project["pendingJson"])
		if !everUploaded {
			c.badRequest("upload the project before sharing it")
			return
		}
	}

	if target == "public" {
		firstShare = toFloat(project["sharedAt"]) == 0
		project["shared"] = true
		project["visibility"] = "public"
		if firstShare {
			project["sharedAt"] = timestamp()
		}
	} else if target == "unlisted" {
		project["shared"] = false
		project["visibility"] = "unlisted"
	} else {
		project["shared"] = false
		project["visibility"] = "private"
		project["viewKey"] = randomString(16)
	}

	project["edited"] = timestamp()
	saveProject(project)
	upsertIndexEntry(project)

	if target == "private" {
		profile := loadProfile(toString(project["owner"]))
		if toString(profile["featuredProject"]) == toString(project["id"]) {
			profile["featuredProject"] = ""
			saveProfile(profile)
		}
	}
	if firstShare {
		addActivity(toString(project["owner"]), "share", Obj{"projectId": project["id"], "projectTitle": project["title"]})
	}
	c.json(200, Obj{"ok": true, "project": projectResponse(project, c)})
}

func recordSale(project Obj, buyer string, price, sellerAmount float64) {
	buyers := projectBuyers(project)
	buyers = append(buyers, Obj{"user": buyer, "at": timestamp(), "amount": price})
	project["buyers"] = buyers
	project["revenue"] = projectRevenue(project) + sellerAmount

	sales := Obj{}
	if project["saleHistory"] != nil {
		sales = toObj(project["saleHistory"])
	}
	dayKey := todayIndex()
	dayTotal := float64(0)
	if sales[dayKey] != nil {
		dayTotal = toFloat(sales[dayKey])
	}
	sales[dayKey] = dayTotal + sellerAmount
	project["saleHistory"] = sales
}

func handlePurchaseIntent(c *Context) {
	if bilupRoturToken == "" {
		c.json(503, Obj{"ok": false, "error": "purchases are not available right now"})
		return
	}
	res := loadProject(c.param("id"))
	if !res.isOk() {
		c.notFound("project not found")
		return
	}
	project := toObj(res.unwrap())
	if !projectIsViewable(project) {
		c.notFound("project not found")
		return
	}
	price := projectPrice(project)
	if price <= 0 {
		c.badRequest("this project is not for sale")
		return
	}
	if canEditProject(project, c) {
		c.badRequest("you already own this project")
		return
	}
	buyerLower := normalizeUsername(c.getString("username"))
	if hasBought(project, buyerLower) {
		c.json(200, Obj{"ok": true, "already": true, "project": projectResponse(project, c)})
		return
	}

	payTo := bilupAccountUser()
	if payTo == "" {
		c.internalError("could not start the purchase, try again")
		return
	}

	key := "mwbuy_" + randomString(16)
	store := prunePurchaseKeys(loadPurchaseKeys())
	keys := toObj(store["keys"])
	keys[key] = Obj{"projectId": project["id"], "buyer": buyerLower, "price": price, "at": timestamp()}
	store["keys"] = keys
	savePurchaseKeys(store)

	c.json(200, Obj{"ok": true, "key": key, "payTo": payTo, "amount": price})
}

func handlePurchaseConfirm(c *Context) {
	if bilupRoturToken == "" {
		c.json(503, Obj{"ok": false, "error": "purchases are not available right now"})
		return
	}
	res := loadProject(c.param("id"))
	if !res.isOk() {
		c.notFound("project not found")
		return
	}
	project := toObj(res.unwrap())
	price := projectPrice(project)
	if price <= 0 {
		c.badRequest("this project is not for sale")
		return
	}
	buyer := c.getString("username")
	buyerLower := normalizeUsername(buyer)
	if hasBought(project, buyerLower) {
		c.json(200, Obj{"ok": true, "already": true, "project": projectResponse(project, c)})
		return
	}

	body := c.bodyJSON()
	key := toString(body["key"])
	if key == "" {
		c.badRequest("missing purchase key")
		return
	}

	store := loadPurchaseKeys()
	keys := toObj(store["keys"])
	pendingRm, ok := keys[key]
	if !ok {
		c.badRequest("this purchase expired, start again")
		return
	}
	pending := toObj(pendingRm)
	if toString(pending["projectId"]) != toString(project["id"]) || normalizeUsername(toString(pending["buyer"])) != buyerLower {
		c.badRequest("this purchase key does not match")
		return
	}
	owed := toFloat(pending["price"])

	if !verifyIncomingTransfer(buyerLower, key, owed) {
		c.json(402, Obj{"ok": false, "pending": true, "error": "payment not received yet, try again in a moment"})
		return
	}

	seller := toString(project["owner"])
	sellerAmount := owed
	if paywallFeePercent > 0 {
		sellerAmount = owed - (owed * paywallFeePercent / 100)
	}
	paidOut := payoutTo(seller, sellerAmount, "Bilup sale")

	delete(keys, key)
	store["keys"] = keys
	savePurchaseKeys(store)

	if !paidOut {
		fmtLog("WARNING: paywall payout failed for project " + toString(project["id"]) + " to " + seller + " amount " + formatFloat(sellerAmount))
		recordPendingPayout(toString(project["id"]), seller, buyer, sellerAmount)
	}

	recordSale(project, buyer, owed, sellerAmount)
	if addToLibrary(buyerLower, toString(project["id"])) {
		project["saves"] = projectSaves(project) + 1
	}
	saveProject(project)
	upsertIndexEntry(project)
	addPurchaseRecord(buyerLower, Obj{"projectId": project["id"], "title": project["title"], "seller": seller, "amount": owed, "at": timestamp()})
	addNotification(seller, buyer, "purchase", Obj{"projectId": project["id"], "projectTitle": project["title"], "amount": owed})

	c.json(200, Obj{"ok": true, "paidOut": paidOut, "project": projectResponse(project, c)})
}
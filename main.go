package main

// main.go — wiring hub (maps main.osl).

func initCaches() {
	viewDedup = cacheCreate(100000, 86400)
	viewIndexFill = cacheCreate(100000, 600)
	userIdsCache = cacheCreate(10000, 300)
	sessionCache = cacheCreate(10000, 3600)
	bansCache = cacheCreate(2, 300)
	adminsCache = cacheCreate(2, 300)
}

func main() {
	initCaches()
	loadDotenv()
	loadConfig()

	ensureDirectories()
	refreshIndexUrls()
	if r2Local {
		fmtLog("R2 not configured; storing blobs on local disk at " + localBlobsDir)
	}
	cleanupExpiredSessions()
	sweepStagedProjects()
	go reconcileLoop()
	go stagedFlushLoop()

	app := newRouter()
	app.use(corsMiddleware)
	app.use(loggerMiddleware())

	app.static("/thumbnails", "./data/thumbnails")
	app.GET("/blobs/assets/{name}", handleGetAssetBlob)
	app.GET("/blobs/projects/{id}/project.json", handleGetProjectBlob)
	app.GET("/blobs/evidence/{id}/project.json", handleGetEvidenceJson)
	app.GET("/blobs/evidence/{id}/assets/{name}", handleGetEvidenceAsset)
	if r2Local {
		app.static("/blobs", "./data/blobs")
	}

	app.use(authMiddleware)

	api := app.group("/api")
	api.bodyLimit(262144)

	uploads := app.group("/api")
	uploads.bodyLimit(268435456)
	uploads.POST("/projects/{id}/upload", requireAuth, handleUploadProject)
	uploads.POST("/projects/{id}/thumbnail", requireAuth, handleSetThumbnail)
	uploads.POST("/admin/projects/{id}/extensions/index", requireAdmin, handleAdminIndexProjectExtensions)

	api.GET("/health", handleHealth)
	api.POST("/auth", handleRoturAuth)
	api.GET("/me", handleGetMe)
	api.POST("/logout", handleLogout)

	api.GET("/notifications", requireAuth, handleGetNotifications)
	api.POST("/notifications/read", requireAuth, handleReadNotifications)

	api.GET("/me/stats", requireAuth, handleGetMyStats)
	api.GET("/me/library", requireAuth, handleGetMyLibrary)
	api.GET("/me/purchases", requireAuth, handleGetMyPurchases)
	api.GET("/me/settings", requireAuth, handleGetMySettings)
	api.PUT("/me/settings", requireAuth, handlePutMySettings)

	api.GET("/agreement", handleGetAgreement)
	api.POST("/agreement/accept", requireAuth, handleAcceptAgreement)

	api.GET("/news", handleGetNews)
	api.POST("/news", requireAdmin, handleCreateNews)
	api.DELETE("/news/{id}", requireAdmin, handleDeleteNews)
	api.POST("/news/{id}/react", requireAuth, handleReactNews)

	api.GET("/search/users", handleSearchUsers)
	api.GET("/users/{name}", handleGetUser)
	api.PUT("/me/profile", requireAuth, handleUpdateProfile)
	api.POST("/users/{name}/follow", requireAuth, handleFollowUser)
	api.DELETE("/users/{name}/follow", requireAuth, handleUnfollowUser)
	api.GET("/users/{name}/projects", handleGetUserProjects)
	api.GET("/users/{name}/loves", handleGetUserLoves)
	api.GET("/explore", handleExplore)
	api.GET("/leaderboard", handleLeaderboard)
	api.GET("/projects/{id}/remixes", handleGetRemixes)
	api.GET("/projects/{id}/remixtree", handleGetRemixTree)
	api.GET("/activity", handleGetActivity)
	api.GET("/users/{name}/followers", handleGetUserFollowers)
	api.GET("/users/{name}/following", handleGetUserFollowing)
	api.GET("/users/{name}/comments", handleGetProfileComments)
	api.POST("/users/{name}/comments", requireAuth, requireGoodStanding, handleCreateProfileComment)
	api.DELETE("/users/{name}/comments/{cid}", requireAuth, handleDeleteProfileComment)
	api.POST("/users/{name}/comments/{cid}/react", requireAuth, handleReactProfileComment)

	api.GET("/projects/{id}/comments", handleGetProjectComments)
	api.POST("/projects/{id}/comments", requireAuth, requireGoodStanding, handleCreateProjectComment)
	api.DELETE("/projects/{id}/comments/{cid}", requireAuth, handleDeleteProjectComment)
	api.POST("/projects/{id}/comments/{cid}/react", requireAuth, handleReactProjectComment)

	api.POST("/projects/{id}/visibility", requireAuth, requireGoodStanding, handleSetVisibility)
	api.GET("/projects/{id}/commits", handleGetCommits)
	api.GET("/projects/{id}/pulls", handleListPulls)
	api.POST("/projects/{id}/pulls", requireAuth, handleCreatePull)
	api.GET("/projects/{id}/pulls/{index}", handleGetPull)
	api.GET("/projects/{id}/pulls/{index}/diff", handleGetPullDiff)
	api.POST("/projects/{id}/pulls/{index}/merge", requireAuth, handleMergePull)
	api.POST("/projects/{id}/purchase/intent", requireAuth, handlePurchaseIntent)
	api.POST("/projects/{id}/purchase/confirm", requireAuth, handlePurchaseConfirm)
	api.POST("/projects/{id}/save", requireAuth, handleSaveProject)
	api.DELETE("/projects/{id}/save", requireAuth, handleUnsaveProject)
	api.GET("/projects/{id}/extensions/{hash}/source", handleGetProjectExtensionSource)

	api.GET("/me/quota", requireAuth, handleGetMyQuota)
	api.POST("/me/quota/reset", requireAuth, handleQuotaReset)
	api.POST("/me/quota/reset/confirm", requireAuth, handleQuotaResetConfirm)
	api.POST("/projects", requireAuth, handleCreateProject)
	api.POST("/projects/{id}/assets/check", requireAuth, handleCheckProjectAssets)
	api.GET("/projects/{id}/project.json", handleGetProjectJson)
	api.GET("/projects/{id}/editor", handleGetEditorProject)
	api.GET("/projects/{id}", handleGetProject)
	api.PUT("/projects/{id}", requireAuth, handleUpdateProject)
	api.DELETE("/projects/{id}", requireAuth, handleDeleteProject)
	api.POST("/projects/{id}/publish", requireAuth, requireGoodStanding, handlePublishProject)
	api.POST("/projects/{id}/unpublish", requireAuth, handleUnpublishProject)
	api.POST("/projects/{id}/view", handleViewProject)
	api.POST("/projects/{id}/react", requireAuth, handleReactProject)
	api.POST("/projects/{id}/remix", requireAuth, handleRemixProject)
	api.POST("/admin/standing", requireAdmin, handleSetStanding)
	api.GET("/admin/user", requireAdmin, handleGetUserAdmin)
	api.POST("/admin/user/message", requireAdmin, handleMessageUser)
	api.POST("/admin/user/profile", requireAdmin, handleAdminUpdateProfile)
	api.GET("/admin/agreement", requireAdmin, handleAdminGetAgreement)
	api.POST("/admin/agreement", requireAdmin, handleAdminUpdateAgreement)
	api.POST("/admin/reconcile-deletions", requireAdmin, handleReconcileDeletions)
	api.POST("/report", requireAuth, handleCreateReport)
	api.POST("/reports", requireAuth, handleCreateReport)
	api.GET("/admin/reports", requireAdmin, handleGetReports)
	api.GET("/admin/reports/evidence/{id}", requireAdmin, handleGetReportEvidence)
	api.POST("/admin/reports/resolve", requireAdmin, handleResolveReport)
	api.POST("/admin/reports/action", requireAdmin, handleReportAction)
	api.GET("/admin/admins", requireAdmin, handleListAdmins)
	api.POST("/admin/admins", requireAdmin, handleAddAdmin)
	api.POST("/admin/admins/remove", requireAdmin, handleRemoveAdmin)
	api.GET("/admin/bans", requireAdmin, handleListBans)
	api.POST("/admin/ban", requireAdmin, handleBanUser)
	api.POST("/admin/unban", requireAdmin, handleUnbanUser)
	api.GET("/admin/projects", requireAdmin, handleAdminSearchProjects)
	api.GET("/admin/stats", requireAdmin, handleAdminStats)
	api.GET("/admin/users", requireAdmin, handleListUsers)

	api.GET("/admin/payouts", requireAdmin, handleListPendingPayouts)
	api.POST("/admin/payouts/retry", requireAdmin, handleRetryPendingPayouts)
	api.GET("/admin/extensions", requireAdmin, handleAdminGetExtensions)
	api.GET("/admin/extensions/{hash}/source", requireAdmin, handleAdminGetExtensionSource)
	api.POST("/admin/extensions/policy", requireAdmin, handleAdminSetExtensionPolicy)
	api.POST("/admin/extensions/url-policy", requireAdmin, handleAdminSetExtensionUrlPolicy)

	// ---- Community tools (spaces, releases, previews, diagnostics, etc.) ----
	api.GET("/spaces", handleListSpaces)
	api.GET("/spaces/{id}", handleGetSpace)
	api.POST("/spaces", requireAuth, handleCreateSpace)
	api.PUT("/spaces/{id}", requireAuth, handleUpdateSpace)
	api.DELETE("/spaces/{id}", requireAuth, handleDeleteSpace)
	api.POST("/spaces/{id}/projects/{project}", requireAuth, handleAddSpaceProject)
	api.DELETE("/spaces/{id}/projects/{project}", requireAuth, handleRemoveSpaceProject)
	api.POST("/spaces/{id}/follow", requireAuth, handleFollowSpace)
	api.DELETE("/spaces/{id}/follow", requireAuth, handleUnfollowSpace)
	api.GET("/me/spaces", requireAuth, handleMySpaces)
	api.GET("/spaces/{id}/management", requireAuth, handleGetSpaceManagement)
	api.POST("/spaces/{id}/invite", requireAuth, handleInviteSpaceCurator)
	api.POST("/spaces/{id}/respond-invite", requireAuth, handleRespondSpaceCuratorInvite)
	api.DELETE("/spaces/{id}/curators/{username}", requireAuth, handleRemoveSpaceCurator)
	api.DELETE("/spaces/{id}/invite/{username}", requireAuth, handleCancelSpaceCuratorInvite)
	api.POST("/spaces/{id}/react", requireAuth, handleReactSpace)
	api.GET("/spaces/{id}/comments", handleGetSpaceComments)
	api.POST("/spaces/{id}/comments", requireAuth, requireGoodStanding, handleCreateSpaceComment)
	api.DELETE("/spaces/{id}/comments/{cid}", requireAuth, handleDeleteSpaceComment)
	api.POST("/spaces/{id}/comments/{cid}/react", requireAuth, handleReactSpaceComment)

	api.POST("/projects/{id}/preview", requireAuth, handleCreatePreview)

	api.GET("/projects/{id}/releases", handleListReleases)
	api.POST("/projects/{id}/releases", requireAuth, handleCreateRelease)
	api.GET("/projects/{id}/releases/{release}/project.json", handleGetReleaseProjectJson)

	api.POST("/projects/{id}/contributions", requireAuth, handleCreateContribution)

	api.POST("/projects/{id}/diagnostics", handleRecordDiagnostic)
	api.GET("/projects/{id}/diagnostics", requireAuth, handleGetDiagnostics)

	api.POST("/projects/{id}/feedback", handleCreateProjectFeedback)
	api.GET("/projects/{id}/feedback", requireAuth, handleGetProjectFeedback)
	api.PUT("/projects/{id}/feedback/{feedback}", requireAuth, handleUpdateProjectFeedback)

	api.GET("/projects/{id}/reviews", handleListProjectReviews)
	api.GET("/users/{name}/reviews", handleListUserReviews)
	api.PUT("/projects/{id}/review", requireAuth, handleSaveProjectReview)
	api.DELETE("/projects/{id}/review", requireAuth, handleDeleteProjectReview)

	api.GET("/ideas", handleListIdeas)
	api.POST("/ideas", requireAuth, handleCreateIdea)
	api.POST("/ideas/{id}/vote", requireAuth, handleVoteIdea)
	api.GET("/ideas/{id}/comments", handleGetIdeaComments)
	api.POST("/ideas/{id}/comments", requireAuth, requireGoodStanding, handleCreateIdeaComment)
	api.DELETE("/ideas/{id}/comments/{cid}", requireAuth, handleDeleteIdeaComment)
	api.PUT("/ideas/{id}", requireAuth, handleUpdateIdea)

	api.GET("/projects/{id}/card", handleProjectCard)

	fmtLog("Bilup API listening on " + appURL)
	if err := app.serve(":" + port); err != nil {
		panic(err)
	}
}
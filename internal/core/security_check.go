package core

import (
	"strings"

	"go.owlkei.com/owlkei/internal/common"
)

const securityCheckDocs = "https://github.com/owlkei/go#production-checklist"

func (app *App) securityCheck() {
	hasSubmit, hasRender := false, false
	for _, h := range app.container.httpMainHandlers {
		switch common.PageOperation(h.MainHandlerName) {
		case common.PageSubmit:
			hasSubmit = true
		case common.PageRender:
			hasRender = true
		}
	}
	var missing []string
	if hasSubmit && !app.isCSRFSet {
		missing = append(missing, "csrf")
	}
	if hasRender && !app.isHeadersSet {
		missing = append(missing, "headers")
	}
	if len(missing) == 0 {
		return
	}
	app.logger.Warn("SecurityCheck", "missing", strings.Join(missing, ","), "docs", securityCheckDocs)
}

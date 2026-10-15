package app

import (
	"fmt"

	"lum-harbor-notifier/internal/dashboardview"
)

// GetDashboardView returns the one Dashboard-specific application projection.
// App performs transport parsing/mapping only; dashboardview.Service owns the
// cross-domain read composition.
func (app *App) GetDashboardView(request DashboardViewRequestDTO) (DashboardViewDTO, error) {
	if app.dashboardViewService == nil {
		return DashboardViewDTO{}, fmt.Errorf("dashboard view service is not initialized")
	}
	plannerRequest, err := parsePlannerViewRequest(
		request.RangeStart,
		request.RangeEnd,
		request.Today,
		request.Timezone,
	)
	if err != nil {
		return DashboardViewDTO{}, err
	}
	view, err := app.dashboardViewService.Get(appContext(app.ctx), dashboardview.Request{Planner: plannerRequest})
	if err != nil {
		return DashboardViewDTO{}, fmt.Errorf("get Dashboard View: %w", err)
	}
	return dashboardViewToDTO(view), nil
}

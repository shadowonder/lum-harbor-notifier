import { GetDashboardView } from '../../wailsjs/go/app/App';
import { mapDashboardViewDTO, type DashboardViewApi } from './dashboardApi';

export const dashboardViewApi: DashboardViewApi = {
  async get(request) {
    return mapDashboardViewDTO(await GetDashboardView(request));
  },
};

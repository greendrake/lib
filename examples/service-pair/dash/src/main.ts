import { TabDashboard, createDash, type DashTab } from '@greendrake/dash'
import { ServiceStateControl } from '@greendrake/dash/service-state'
import { api, states } from './api'

// The services the API registers its mocked machines under, by their tabs'
// labels: a tab, and a control, each.
const SERVICES = { web: 'Web', worker: 'Worker' }

const tabs: DashTab[] = Object.entries(SERVICES).map(([service, label]) => ({
    id: service,
    label,
    component: ServiceStateControl,
    props: {
        service,
        api,
        states
    }
}))

createDash({
    splash: ['SERVICE'],
    defaultTitle: 'Service',
    routes: [
        {
            path: '/',
            name: 'Dashboard',
            component: TabDashboard,
            props: { tabs }
        }
    ]
})

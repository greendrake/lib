import { TabDashboard, createDash, type DashTab } from '@greendrake/dash'
import { ServiceStateControl } from '@greendrake/dash/service-state'
import { api, transport } from './api'

const tabs: DashTab[] = [
    {
        id: 'service',
        label: 'Service',
        component: ServiceStateControl,
        props: {
            // The name the API registers its mocked service under.
            service: 'mock',
            api,
            transport
        }
    }
]

createDash({
    splash: ['SERVICE'],
    routes: [
        {
            path: '/',
            name: 'Dashboard',
            component: TabDashboard,
            props: { tabs }
        }
    ]
})

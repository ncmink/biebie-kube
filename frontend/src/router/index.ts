import { createRouter, createWebHashHistory } from 'vue-router'

/**
 * Routes are hash-based because the application is served from an embedded
 * asset bundle with no server to rewrite paths.
 */
const router = createRouter({
  history: createWebHashHistory(),
  routes: [
    {
      path: '/',
      name: 'clusters',
      component: () => import('@/views/Dashboard.vue'),
    },
    {
      path: '/cluster/:clusterId',
      name: 'cluster',
      component: () => import('@/views/Cluster.vue'),
      props: true,
      children: [
        {
          path: '',
          name: 'overview',
          component: () => import('@/views/Overview.vue'),
          props: true,
        },
        {
          path: 'r/:kind',
          name: 'resources',
          component: () => import('@/views/ResourceList.vue'),
          props: true,
        },
        {
          path: 'r/:kind/:namespace/:name',
          name: 'resource',
          component: () => import('@/views/ResourceDetail.vue'),
          props: true,
        },
      ],
    },
    {
      path: '/forwards',
      name: 'forwards',
      component: () => import('@/views/PortForwards.vue'),
    },
    {
      path: '/settings',
      name: 'settings',
      component: () => import('@/views/Settings.vue'),
    },
    { path: '/:pathMatch(.*)*', redirect: '/' },
  ],
})

export default router

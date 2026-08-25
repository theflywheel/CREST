# One journey app as its own deployment — the reference's products ship
# separately (a wallet, a field app, a console, a verify page), and these
# doors mirror that: APP selects which app this service is. The app is served
# at the domain root with the shared design system beside it at /shared, which
# is exactly the sibling layout the apps' relative imports resolve against.
# Same nginx template as the combined door: same /api proxy allowlist, same
# /internal fence.
FROM nginx:1.27-alpine
ARG APP
COPY apps/shared       /usr/share/nginx/html/shared
COPY apps/${APP}       /usr/share/nginx/html
COPY infra/railway/nginx.apps.conf.template /etc/nginx/templates/default.conf.template
# Railway injects PORT; the stock nginx entrypoint substitutes ${PORT}.

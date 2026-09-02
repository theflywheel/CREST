# One journey app as its own deployment — the reference's products ship
# separately (a wallet, a field app, a console, a verify page), and these
# doors mirror that: APP selects which app this service is. The app is served
# at the domain root; the rebuilt doors (React/Vite, frontend/ workspace, a
# relative asset base so root and /app/ serving both work) build in the
# webapps stage, while doors not yet ported ship as the static files under
# apps/ with the shared design system beside them at /shared.
# Same nginx template as the combined door: same /api proxy allowlist, same
# /internal fence.
FROM node:22-alpine AS webapps
ENV COREPACK_ENABLE_DOWNLOAD_PROMPT=0
RUN corepack enable
COPY frontend /frontend
WORKDIR /frontend
RUN pnpm install --frozen-lockfile && pnpm -r build
COPY apps /apps-src
RUN mkdir -p /site \
    && cp -R /apps-src/enrolment /site/enrolment \
    && cp -R /apps-src/console   /site/console \
    && cp -R /frontend/apps/verify/dist /site/verify \
    && cp -R /frontend/apps/worker/dist /site/worker

FROM nginx:1.27-alpine
ARG APP
COPY apps/shared /usr/share/nginx/html/shared
COPY --from=webapps /site/${APP} /usr/share/nginx/html
COPY infra/railway/nginx.apps.conf.template /etc/nginx/templates/default.conf.template
# Railway injects PORT; the stock nginx entrypoint substitutes ${PORT}.

FROM oven/bun:1 AS build
WORKDIR /src
COPY package.json bun.lock ./
RUN bun install --frozen-lockfile
COPY . .
ARG VITE_SITES="site-1=Site 1"
ENV VITE_DATA_SOURCE=live VITE_SITES=${VITE_SITES}
RUN bun run build

FROM nginx:1.29-alpine
COPY nginx.conf /etc/nginx/conf.d/default.conf
COPY --from=build /src/dist /usr/share/nginx/html

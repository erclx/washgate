FROM php:8.3-cli
RUN docker-php-ext-install pdo_mysql
COPY --from=composer:2 /usr/bin/composer /usr/bin/composer
WORKDIR /app
COPY composer.json composer.lock ./
RUN composer install --no-dev --no-interaction --no-progress --optimize-autoloader
COPY . .
EXPOSE 8080
CMD ["php", "-S", "0.0.0.0:8080", "-t", "public", "public/index.php"]

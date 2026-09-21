<?php

declare(strict_types=1);

use Washgate\Invoicing\Database\DatabaseConfig;
use Washgate\Invoicing\Http\InvoiceEndpoint;
use Washgate\Invoicing\Invoice\FleetInvoiceReader;
use Washgate\Invoicing\Invoice\InvoiceCsv;

require __DIR__ . '/../vendor/autoload.php';

$config = DatabaseConfig::fromEnvironment(getenv());
$endpoint = new InvoiceEndpoint(
    new FleetInvoiceReader($config->connect(...)),
    new InvoiceCsv(),
);

$path = parse_url((string) ($_SERVER['REQUEST_URI'] ?? '/'), PHP_URL_PATH);
$response = $endpoint->handle((string) ($_SERVER['REQUEST_METHOD'] ?? 'GET'), is_string($path) ? $path : '/', $_GET);

http_response_code($response->status);
foreach ($response->headers as $name => $value) {
    header("{$name}: {$value}");
}
echo $response->body;

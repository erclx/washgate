<?php

declare(strict_types=1);

namespace Washgate\Invoicing\Http;

use Closure;
use InvalidArgumentException;
use PDOException;
use Washgate\Invoicing\Invoice\BillingMonth;
use Washgate\Invoicing\Invoice\FleetInvoiceReader;
use Washgate\Invoicing\Invoice\InvoiceCsv;
use Washgate\Invoicing\Invoice\MissingPriceException;

/** Serves `GET /invoices/<YYYY-MM>.csv`. */
final class InvoiceEndpoint
{
    private const string ROUTE = '#^/invoices/([^/]+)\.csv$#';

    /** @var Closure(string): void */
    private readonly Closure $log;

    /** @param (Closure(string): void)|null $log receives one plate-free line per request, stderr by default */
    public function __construct(
        private readonly FleetInvoiceReader $reader,
        private readonly InvoiceCsv $csv,
        ?Closure $log = null,
    ) {
        $this->log = $log ?? static function (string $line): void {
            error_log($line);
        };
    }

    /** @param array<string, mixed> $query */
    public function handle(string $method, string $path, array $query): Response
    {
        if (preg_match(self::ROUTE, $path, $matches) !== 1) {
            return Response::text(404, 'Not found');
        }
        if ($method !== 'GET') {
            return new Response(405, ['Allow' => 'GET', 'Content-Type' => 'text/plain; charset=utf-8'], "Method not allowed\n");
        }

        $split = $query['split'] ?? null;
        if ($split !== null && $split !== 'leasing') {
            return $this->finish($matches[1], 'invalid', 0, Response::text(400, "Unknown split, expected 'leasing'."));
        }
        $splitMode = $split === null ? 'none' : 'leasing';

        try {
            $month = BillingMonth::fromString($matches[1]);
        } catch (InvalidArgumentException $error) {
            return $this->finish($matches[1], $splitMode, 0, Response::text(400, $error->getMessage()));
        }

        try {
            $lines = $this->reader->read($month);
        } catch (MissingPriceException $error) {
            return $this->finish($matches[1], $splitMode, 0, Response::text(422, $error->getMessage()));
        } catch (PDOException $error) {
            $this->log('invoice month=' . $matches[1] . ' error=' . $error::class);

            return $this->finish($matches[1], $splitMode, 0, Response::text(500, 'Internal error'));
        }

        $body = $this->csv->render($lines, $splitMode === 'leasing');

        return $this->finish($matches[1], $splitMode, count($lines), new Response(200, ['Content-Type' => 'text/csv; charset=utf-8'], $body));
    }

    private function finish(string $month, string $splitMode, int $lineCount, Response $response): Response
    {
        $this->log("invoice month={$month} split={$splitMode} lines={$lineCount} status={$response->status}");

        return $response;
    }

    private function log(string $line): void
    {
        ($this->log)($line);
    }
}

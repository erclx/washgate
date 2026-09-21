<?php

declare(strict_types=1);

namespace Washgate\Invoicing\Tests\Http;

use PDO;
use PHPUnit\Framework\TestCase;
use Washgate\Invoicing\Http\InvoiceEndpoint;
use Washgate\Invoicing\Invoice\FleetInvoiceReader;
use Washgate\Invoicing\Invoice\InvoiceCsv;
use Washgate\Invoicing\Tests\Database\LedgerSeeder;
use Washgate\Invoicing\Tests\Database\TemporaryDatabase;

final class InvoiceEndpointTest extends TestCase
{
    private TemporaryDatabase $database;
    private LedgerSeeder $ledger;
    private InvoiceEndpoint $endpoint;

    #[\Override]
    protected function setUp(): void
    {
        $this->database = TemporaryDatabase::create();
        $pdo = $this->database->connection();
        $this->ledger = new LedgerSeeder($pdo);
        $this->endpoint = $this->endpointOver(static fn(): PDO => $pdo);
        $this->ledger->price('fleet_wash', 1000, '2020-01-01 00:00:00');
        $this->ledger->company('c-nord', 'Nordfrakt');
        $this->ledger->vehicle('ABC123', 'c-nord', 'Volvia');
    }

    #[\Override]
    protected function tearDown(): void
    {
        if (isset($this->database)) {
            $this->database->drop();
        }
    }

    public function testServesTheMonthAsCsvByteForByte(): void
    {
        $this->ledger->fleetWash('ABC123', 'c-nord', '2026-10-05 08:00:00');
        $this->ledger->fleetWash('ABC123', 'c-nord', '2026-10-06 08:00:00');

        $response = $this->endpoint->handle('GET', '/invoices/2026-10.csv', []);

        self::assertSame(200, $response->status);
        self::assertSame('text/csv; charset=utf-8', $response->headers['Content-Type']);
        self::assertSame("company,plate,washes,amount_ore\r\nNordfrakt,ABC123,2,2000\r\n", $response->body);
    }

    public function testSplitLeasingAddsTheLeasingColumn(): void
    {
        $this->ledger->fleetWash('ABC123', 'c-nord', '2026-10-05 08:00:00');

        $response = $this->endpoint->handle('GET', '/invoices/2026-10.csv', ['split' => 'leasing']);

        self::assertStringStartsWith("leasing_company,company,plate,washes,amount_ore\r\n", $response->body);
    }

    public function testRejectsAMalformedMonth(): void
    {
        self::assertSame(400, $this->endpoint->handle('GET', '/invoices/2026-13.csv', [])->status);
    }

    public function testRejectsAnUnknownSplitValue(): void
    {
        self::assertSame(400, $this->endpoint->handle('GET', '/invoices/2026-10.csv', ['split' => 'owner'])->status);
    }

    public function testRejectsAMethodOtherThanGet(): void
    {
        $response = $this->endpoint->handle('POST', '/invoices/2026-10.csv', []);

        self::assertSame(405, $response->status);
        self::assertSame('GET', $response->headers['Allow']);
    }

    public function testAnswersNotFoundForAnUnknownPath(): void
    {
        self::assertSame(404, $this->endpoint->handle('GET', '/elsewhere', [])->status);
    }

    public function testAnswers422NamingTheCompanyWhenAWashHasNoPrice(): void
    {
        $this->ledger->fleetWash('ABC123', 'c-nord', '2019-06-10 08:00:00');

        $response = $this->endpoint->handle('GET', '/invoices/2019-06.csv', []);

        self::assertSame(422, $response->status);
        self::assertStringContainsString('Nordfrakt', $response->body);
    }

    public function testAnswers500WithNoDetailWhenTheDatabaseFails(): void
    {
        $endpoint = $this->endpointOver(static fn(): PDO => new PDO('mysql:host=127.0.0.1;port=1', 'nobody', 'secret'));

        $response = $endpoint->handle('GET', '/invoices/2026-10.csv', []);

        self::assertSame(500, $response->status);
        self::assertSame("Internal error\n", $response->body);
    }

    /** @param \Closure(): PDO $connect */
    private function endpointOver(\Closure $connect): InvoiceEndpoint
    {
        return new InvoiceEndpoint(new FleetInvoiceReader($connect), new InvoiceCsv(), static function (string $line): void {});
    }
}

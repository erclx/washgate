<?php

declare(strict_types=1);

namespace Washgate\Invoicing\Tests\Invoice;

use PHPUnit\Framework\TestCase;
use Washgate\Invoicing\Invoice\BillingMonth;
use Washgate\Invoicing\Invoice\FleetInvoiceReader;
use Washgate\Invoicing\Invoice\InvoiceLine;
use Washgate\Invoicing\Invoice\MissingPriceException;
use Washgate\Invoicing\Tests\Database\LedgerSeeder;
use Washgate\Invoicing\Tests\Database\TemporaryDatabase;

final class FleetInvoiceReaderTest extends TestCase
{
    private TemporaryDatabase $database;
    private LedgerSeeder $ledger;
    private FleetInvoiceReader $reader;

    #[\Override]
    protected function setUp(): void
    {
        $this->database = TemporaryDatabase::create();
        $pdo = $this->database->connection();
        $this->ledger = new LedgerSeeder($pdo);
        $this->reader = new FleetInvoiceReader(static fn() => $pdo);
        $this->ledger->price('fleet_wash', 1000, '2020-01-01 00:00:00');
        $this->ledger->company('c-nord', 'Nordfrakt');
        $this->ledger->company('c-arl', 'Arlanda Taxi');
    }

    #[\Override]
    protected function tearDown(): void
    {
        if (isset($this->database)) {
            $this->database->drop();
        }
    }

    public function testCountsAndPricesOneLinePerCarPerCompany(): void
    {
        $this->ledger->vehicle('ABC123', 'c-nord', 'Volvia');
        $this->ledger->fleetWash('ABC123', 'c-nord', '2026-10-05 08:00:00');
        $this->ledger->fleetWash('ABC123', 'c-nord', '2026-10-06 08:00:00');
        $this->ledger->fleetWash('ABC123', 'c-nord', '2026-10-07 08:00:00');

        $lines = $this->reader->read(BillingMonth::fromString('2026-10'));

        self::assertEquals([new InvoiceLine('Nordfrakt', 'ABC123', 'Volvia', 3, 3000)], $lines);
    }

    public function testCarBilledToTwoCompaniesMakesTwoLines(): void
    {
        $this->ledger->vehicle('ABC123', 'c-arl', null);
        $this->ledger->fleetWash('ABC123', 'c-nord', '2026-10-05 08:00:00');
        $this->ledger->fleetWash('ABC123', 'c-arl', '2026-10-06 08:00:00');

        $lines = $this->reader->read(BillingMonth::fromString('2026-10'));

        self::assertSame(['Arlanda Taxi', 'Nordfrakt'], array_map(static fn(InvoiceLine $line): string => $line->companyName, $lines));
    }

    public function testLastStockholmEveningCountsAndNextMorningDoesNot(): void
    {
        $this->ledger->vehicle('ABC123', 'c-nord', null);
        $this->ledger->fleetWash('ABC123', 'c-nord', '2026-10-31 22:30:00');
        $this->ledger->fleetWash('ABC123', 'c-nord', '2026-10-31 23:30:00');

        $lines = $this->reader->read(BillingMonth::fromString('2026-10'));

        self::assertSame(1, $lines[0]->washCount);
    }

    public function testExcludesWashesThatAreNotFleet(): void
    {
        $this->ledger->vehicle('ABC123', null, null);
        $this->ledger->premiumWash('ABC123', '2026-10-05 08:00:00');

        $lines = $this->reader->read(BillingMonth::fromString('2026-10'));

        self::assertSame([], $lines);
    }

    public function testPricesEachWashAtThePriceInForceWhenAdmitted(): void
    {
        $this->ledger->vehicle('ABC123', 'c-nord', null);
        $this->ledger->price('fleet_wash', 1200, '2026-10-15 00:00:00');
        $this->ledger->fleetWash('ABC123', 'c-nord', '2026-10-10 08:00:00');
        $this->ledger->fleetWash('ABC123', 'c-nord', '2026-10-20 08:00:00');

        $lines = $this->reader->read(BillingMonth::fromString('2026-10'));

        self::assertSame(2200, $lines[0]->amountOre);
    }

    public function testRejectsAWashWithNoPriceInForce(): void
    {
        $this->ledger->vehicle('ABC123', 'c-nord', null);
        $this->ledger->fleetWash('ABC123', 'c-nord', '2019-06-10 08:00:00');

        $this->expectException(MissingPriceException::class);

        $this->reader->read(BillingMonth::fromString('2019-06'));
    }

    public function testCarWithNoVehicleRowHasNoLeasingCompany(): void
    {
        $this->ledger->fleetWash('ZZZ999', 'c-nord', '2026-10-05 08:00:00');

        $lines = $this->reader->read(BillingMonth::fromString('2026-10'));

        self::assertNull($lines[0]->leasingCompany);
    }

    public function testEmptyMonthReturnsNoLines(): void
    {
        $lines = $this->reader->read(BillingMonth::fromString('2026-10'));

        self::assertSame([], $lines);
    }
}

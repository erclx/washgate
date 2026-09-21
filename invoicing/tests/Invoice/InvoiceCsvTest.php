<?php

declare(strict_types=1);

namespace Washgate\Invoicing\Tests\Invoice;

use PHPUnit\Framework\TestCase;
use Washgate\Invoicing\Invoice\InvoiceCsv;
use Washgate\Invoicing\Invoice\InvoiceLine;

final class InvoiceCsvTest extends TestCase
{
    public function testWritesHeaderAndOneRowPerLine(): void
    {
        $lines = [new InvoiceLine('Nordfrakt', 'ABC123', 'Volvia', 3, 3000)];

        $csv = (new InvoiceCsv())->render($lines, false);

        self::assertSame("company,plate,washes,amount_ore\r\nNordfrakt,ABC123,3,3000\r\n", $csv);
    }

    public function testSplitAddsLeasingColumnAndOrdersByLeasingCompanyThenCompanyThenPlate(): void
    {
        $lines = [
            new InvoiceLine('Nordfrakt', 'BBB222', 'Volvia', 1, 1000),
            new InvoiceLine('Arlanda Taxi', 'CCC333', 'Alphabet', 1, 1000),
            new InvoiceLine('Nordfrakt', 'AAA111', 'Volvia', 1, 1000),
        ];

        $csv = (new InvoiceCsv())->render($lines, true);

        self::assertSame(
            "leasing_company,company,plate,washes,amount_ore\r\n"
            . "Alphabet,\"Arlanda Taxi\",CCC333,1,1000\r\n"
            . "Volvia,Nordfrakt,AAA111,1,1000\r\n"
            . "Volvia,Nordfrakt,BBB222,1,1000\r\n",
            $csv,
        );
    }

    public function testQuotesNamesWithCommasAndQuotes(): void
    {
        $lines = [new InvoiceLine('Nord, "Frakt"', 'ABC123', null, 1, 1000)];

        $csv = (new InvoiceCsv())->render($lines, false);

        self::assertStringContainsString("\"Nord, \"\"Frakt\"\"\",ABC123,1,1000\r\n", $csv);
    }

    public function testNeutralizesANameThatStartsAFormula(): void
    {
        $lines = [new InvoiceLine('=HYPERLINK("x")', 'ABC123', null, 1, 1000)];

        $csv = (new InvoiceCsv())->render($lines, false);

        self::assertStringContainsString("'=HYPERLINK", $csv);
    }

    public function testEmptyInvoiceIsTheHeaderAlone(): void
    {
        $csv = (new InvoiceCsv())->render([], false);

        self::assertSame("company,plate,washes,amount_ore\r\n", $csv);
    }
}

<?php

declare(strict_types=1);

namespace Washgate\Invoicing\Tests\Invoice;

use InvalidArgumentException;
use PHPUnit\Framework\Attributes\DataProvider;
use PHPUnit\Framework\TestCase;
use Washgate\Invoicing\Invoice\BillingMonth;

final class BillingMonthTest extends TestCase
{
    public function testParsesYearAndMonth(): void
    {
        $month = BillingMonth::fromString('2026-10');

        self::assertSame('2026-10-01', $month->firstDay()->format('Y-m-d'));
        self::assertSame('2026-10-31', $month->lastDay()->format('Y-m-d'));
    }

    #[DataProvider('invalidMonths')]
    public function testRejectsMalformedMonth(string $input): void
    {
        $this->expectException(InvalidArgumentException::class);

        BillingMonth::fromString($input);
    }

    /** @return iterable<string, array{string}> */
    public static function invalidMonths(): iterable
    {
        yield 'month out of range' => ['2026-13'];
        yield 'missing zero pad' => ['2026-1'];
        yield 'not a date' => ['october'];
    }

    #[DataProvider('utcRanges')]
    public function testRangeIsHalfOpenBetweenStockholmMidnightsInUtc(string $input, string $start, string $end): void
    {
        $month = BillingMonth::fromString($input);

        self::assertSame($start, $month->startUtc()->format('Y-m-d H:i:s'));
        self::assertSame($end, $month->endUtc()->format('Y-m-d H:i:s'));
    }

    /** @return iterable<string, array{string, string, string}> */
    public static function utcRanges(): iterable
    {
        yield 'summer time start' => ['2026-09', '2026-08-31 22:00:00', '2026-09-30 22:00:00'];
        yield 'clock change inside the month' => ['2026-10', '2026-09-30 22:00:00', '2026-10-31 23:00:00'];
        yield 'winter time' => ['2026-11', '2026-10-31 23:00:00', '2026-11-30 23:00:00'];
        yield 'december into january' => ['2026-12', '2026-11-30 23:00:00', '2026-12-31 23:00:00'];
    }
}

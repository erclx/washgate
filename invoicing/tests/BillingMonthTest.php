<?php

declare(strict_types=1);

namespace Washgate\Invoicing\Tests;

use InvalidArgumentException;
use PHPUnit\Framework\Attributes\DataProvider;
use PHPUnit\Framework\TestCase;
use Washgate\Invoicing\BillingMonth;

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
}

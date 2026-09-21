<?php

declare(strict_types=1);

namespace Washgate\Invoicing\Invoice;

use DateTimeImmutable;
use DateTimeZone;
use InvalidArgumentException;

/** One calendar month of fleet washes, in Swedish time. */
final class BillingMonth
{
    private function __construct(private readonly DateTimeImmutable $firstDay) {}

    public static function fromString(string $month): self
    {
        if (preg_match('/^\d{4}-(0[1-9]|1[0-2])$/', $month) !== 1) {
            throw new InvalidArgumentException("Billing month must look like 2026-10, got '{$month}'.");
        }
        $firstDay = DateTimeImmutable::createFromFormat(
            '!Y-m-d',
            "{$month}-01",
            new DateTimeZone('Europe/Stockholm'),
        );
        if ($firstDay === false) {
            throw new InvalidArgumentException("Billing month '{$month}' is not a valid date.");
        }

        return new self($firstDay);
    }

    public function firstDay(): DateTimeImmutable
    {
        return $this->firstDay;
    }

    public function lastDay(): DateTimeImmutable
    {
        return $this->firstDay->modify('last day of this month');
    }

    /** The first instant of the month, the inclusive start of its range. */
    public function startUtc(): DateTimeImmutable
    {
        return $this->firstDay->setTimezone(new DateTimeZone('UTC'));
    }

    /** The first instant of the next month, the exclusive end of its range. */
    public function endUtc(): DateTimeImmutable
    {
        return $this->firstDay->modify('first day of next month')->setTimezone(new DateTimeZone('UTC'));
    }
}

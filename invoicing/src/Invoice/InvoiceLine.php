<?php

declare(strict_types=1);

namespace Washgate\Invoicing\Invoice;

/** One car's washes for one company in a month, priced in öre. */
final readonly class InvoiceLine
{
    public function __construct(
        public string $companyName,
        public string $plate,
        public ?string $leasingCompany,
        public int $washCount,
        public int $amountOre,
    ) {}
}

<?php

declare(strict_types=1);

namespace Washgate\Invoicing\Invoice;

use RuntimeException;

final class MissingPriceException extends RuntimeException
{
    public static function forCompany(string $companyName): self
    {
        return new self("A wash for '{$companyName}' has no fleet_wash price in force when it was admitted.");
    }
}

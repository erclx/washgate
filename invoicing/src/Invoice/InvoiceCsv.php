<?php

declare(strict_types=1);

namespace Washgate\Invoicing\Invoice;

use RuntimeException;

/** Renders invoice lines as RFC 4180 CSV. */
final class InvoiceCsv
{
    private const array FORMULA_LEADS = ['=', '+', '-', '@', "\t", "\r"];

    /** @param list<InvoiceLine> $lines */
    public function render(array $lines, bool $isSplitByLeasingCompany): string
    {
        if ($isSplitByLeasingCompany) {
            usort($lines, static fn(InvoiceLine $a, InvoiceLine $b): int => [$a->leasingCompany ?? '', $a->companyName, $a->plate]
                <=> [$b->leasingCompany ?? '', $b->companyName, $b->plate]);
        }

        $stream = fopen('php://memory', 'w+');
        if ($stream === false) {
            throw new RuntimeException('Cannot open a memory stream to render the invoice.');
        }
        $this->writeRow($stream, $isSplitByLeasingCompany
            ? ['leasing_company', 'company', 'plate', 'washes', 'amount_ore']
            : ['company', 'plate', 'washes', 'amount_ore']);
        foreach ($lines as $line) {
            $row = [$this->neutralize($line->companyName), $this->neutralize($line->plate), $line->washCount, $line->amountOre];
            if ($isSplitByLeasingCompany) {
                array_unshift($row, $this->neutralize($line->leasingCompany ?? ''));
            }
            $this->writeRow($stream, $row);
        }
        rewind($stream);
        $csv = stream_get_contents($stream);
        fclose($stream);

        return $csv === false ? throw new RuntimeException('Cannot read back the rendered invoice.') : $csv;
    }

    /**
     * @param resource                $stream
     * @param list<string|int>        $row
     */
    private function writeRow($stream, array $row): void
    {
        fputcsv($stream, $row, ',', '"', '', "\r\n");
    }

    private function neutralize(string $cell): string
    {
        return $cell !== '' && in_array($cell[0], self::FORMULA_LEADS, true) ? "'" . $cell : $cell;
    }
}

from pathlib import Path

from plate_reader.reader import AlprReader

FIXTURE = Path(__file__).parent / 'fixtures' / '31-Volvo-S90-3.0-15285826772-.jpg'


def test_reads_the_plate_from_a_public_image() -> None:
    reader = AlprReader()

    plate_read = reader.read_plate(FIXTURE.read_bytes())

    assert plate_read is not None
    assert plate_read.plate == 'EEK828'
    assert 0 < plate_read.confidence <= 1
    assert plate_read.box.x1 < plate_read.box.x2
    assert plate_read.box.y1 < plate_read.box.y2

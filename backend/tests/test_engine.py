import pytest

from app.engine.systems.dnd5e import DnD5eSystem


@pytest.fixture
def system():
    return DnD5eSystem()


def test_roll_dice_single(system):
    result = system.roll_dice(sides=20, count=1)
    assert len(result.rolls) == 1
    assert 1 <= result.rolls[0] <= 20
    assert result.total == result.rolls[0]


def test_roll_dice_multiple(system):
    result = system.roll_dice(sides=6, count=3)
    assert len(result.rolls) == 3
    assert all(1 <= roll <= 6 for roll in result.rolls)
    assert result.total == sum(result.rolls)


def test_skill_check_modifier(system):
    result = system.skill_check({"str": 20}, "athletics", difficulty=10)
    assert result.modifier == 5
    assert result.total == result.roll + 5


def test_skill_check_correct_ability(system):
    result = system.skill_check({"dex": 18, "str": 8}, "stealth", difficulty=10)
    assert result.modifier == 4


def test_skill_check_missing_stat(system):
    result = system.skill_check({}, "athletics", difficulty=10)
    assert result.modifier == 0


def test_advantage(system):
    results = [
        system.skill_check({"str": 10}, "athletics", 10, advantage=True)
        for _ in range(50)
    ]
    assert sum(result.roll for result in results) / 50 > 11


def test_disadvantage(system):
    results = [
        system.skill_check({"str": 10}, "athletics", 10, advantage=False)
        for _ in range(50)
    ]
    assert sum(result.roll for result in results) / 50 < 10

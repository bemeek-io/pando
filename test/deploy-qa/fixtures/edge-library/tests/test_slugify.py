from slugkit import slugify


def test_basic():
    assert slugify("Hello, World!") == "hello-world"


def test_accents():
    assert slugify("Crème brûlée") == "creme-brulee"


def test_sep():
    assert slugify("a b c", sep="_") == "a_b_c"

package value

import "testing"

func TestMin_SignedIntegers(t *testing.T) {
	// int
	if Min(5, 3) != 3 {
		t.Error("Min(5, 3) should be 3")
	}

	// int8
	if Min(int8(10), int8(-5)) != int8(-5) {
		t.Error("Min(int8) failed")
	}

	// int16
	if Min(int16(100), int16(50)) != int16(50) {
		t.Error("Min(int16) failed")
	}

	// int32
	if Min(int32(-1), int32(0)) != int32(-1) {
		t.Error("Min(int32) failed")
	}

	// int64
	if Min(int64(1000), int64(999)) != int64(999) {
		t.Error("Min(int64) failed")
	}
}

func TestMin_UnsignedIntegers(t *testing.T) {
	// uint
	if Min(uint(5), uint(3)) != uint(3) {
		t.Error("Min(uint(5), uint(3)) should be 3")
	}

	// uint8
	if Min(uint8(255), uint8(128)) != uint8(128) {
		t.Error("Min(uint8) failed")
	}

	// uint16
	if Min(uint16(1000), uint16(500)) != uint16(500) {
		t.Error("Min(uint16) failed")
	}

	// uint32
	if Min(uint32(100000), uint32(99999)) != uint32(99999) {
		t.Error("Min(uint32) failed")
	}

	// uint64
	if Min(uint64(1<<62), uint64(1<<61)) != uint64(1<<61) {
		t.Error("Min(uint64) failed")
	}
}

func TestMax_SignedIntegers(t *testing.T) {
	// int
	if Max(5, 3) != 5 {
		t.Error("Max(5, 3) should be 5")
	}

	// int8
	if Max(int8(10), int8(-5)) != int8(10) {
		t.Error("Max(int8) failed")
	}

	// int16
	if Max(int16(100), int16(50)) != int16(100) {
		t.Error("Max(int16) failed")
	}

	// int32
	if Max(int32(-1), int32(0)) != int32(0) {
		t.Error("Max(int32) failed")
	}

	// int64
	if Max(int64(1000), int64(999)) != int64(1000) {
		t.Error("Max(int64) failed")
	}
}

func TestMax_UnsignedIntegers(t *testing.T) {
	// uint
	if Max(uint(5), uint(3)) != uint(5) {
		t.Error("Max(uint(5), uint(3)) should be 5")
	}

	// uint8
	if Max(uint8(255), uint8(128)) != uint8(255) {
		t.Error("Max(uint8) failed")
	}

	// uint16
	if Max(uint16(1000), uint16(500)) != uint16(1000) {
		t.Error("Max(uint16) failed")
	}

	// uint32
	if Max(uint32(100000), uint32(99999)) != uint32(100000) {
		t.Error("Max(uint32) failed")
	}

	// uint64
	if Max(uint64(1<<62), uint64(1<<61)) != uint64(1<<62) {
		t.Error("Max(uint64) failed")
	}
}

func TestMin_EqualValues(t *testing.T) {
	if Min(5, 5) != 5 {
		t.Error("Min(5, 5) should be 5")
	}
	if Min(uint64(100), uint64(100)) != uint64(100) {
		t.Error("Min(uint64) with equal values failed")
	}
}

func TestMax_EqualValues(t *testing.T) {
	if Max(5, 5) != 5 {
		t.Error("Max(5, 5) should be 5")
	}
	if Max(uint64(100), uint64(100)) != uint64(100) {
		t.Error("Max(uint64) with equal values failed")
	}
}

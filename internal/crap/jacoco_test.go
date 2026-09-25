package crap

import "testing"

const jacocoSample = `<?xml version="1.0" encoding="UTF-8" standalone="yes"?>
<!DOCTYPE report PUBLIC "-//JACOCO//DTD Report 1.1//EN" "report.dtd">
<report name="demo">
  <package name="com/example/billing">
    <class name="com/example/billing/Pricing" sourcefilename="Pricing.java">
      <method name="applyDiscount" desc="(D)D" line="42">
        <counter type="INSTRUCTION" missed="10" covered="20"/>
        <counter type="BRANCH" missed="2" covered="6"/>
        <counter type="LINE" missed="3" covered="9"/>
        <counter type="COMPLEXITY" missed="2" covered="6"/>
        <counter type="METHOD" missed="0" covered="1"/>
      </method>
      <method name="emptyHelper" desc="()V" line="120">
        <counter type="INSTRUCTION" missed="0" covered="2"/>
        <counter type="LINE" missed="0" covered="1"/>
        <counter type="COMPLEXITY" missed="0" covered="1"/>
        <counter type="METHOD" missed="0" covered="1"/>
      </method>
      <method name="abstractThing" desc="()V" line="130"/>
      <method name="noDebugLine" desc="(I)I">
        <counter type="INSTRUCTION" missed="4" covered="0"/>
        <counter type="COMPLEXITY" missed="1" covered="1"/>
      </method>
    </class>
  </package>
</report>`

func TestParseJacoco(t *testing.T) {
	fns, err := ParseJacoco([]byte(jacocoSample))
	if err != nil {
		t.Fatalf("ParseJacoco: %v", err)
	}
	if len(fns) != 3 {
		t.Fatalf("got %d functions, want 3 (abstract skipped)", len(fns))
	}

	apply := fns[0]
	if apply.File != "com/example/billing/Pricing.java" || apply.Name != "applyDiscount" ||
		apply.StartLine != 42 || apply.Complexity != 8 {
		t.Fatalf("applyDiscount = %+v", apply)
	}
	if apply.Measure != MeasureBranch || apply.Covered != 6 || apply.Total != 8 {
		t.Fatalf("applyDiscount coverage = %v %d/%d, want branch 6/8", apply.Measure, apply.Covered, apply.Total)
	}

	helper := fns[1]
	if helper.Complexity != 1 {
		t.Fatalf("emptyHelper complexity = %d, want 1", helper.Complexity)
	}
	// No BRANCH counter -> falls back to LINE coverage.
	if helper.Measure != MeasureLine || helper.Covered != 1 || helper.Total != 1 {
		t.Fatalf("emptyHelper coverage = %v %d/%d, want line 1/1", helper.Measure, helper.Covered, helper.Total)
	}

	noDebug := fns[2]
	if noDebug.StartLine != 0 {
		t.Fatalf("method without line attr must carry StartLine 0, got %d", noDebug.StartLine)
	}
	if noDebug.Complexity != 2 || noDebug.Measure != MeasureLine {
		t.Fatalf("noDebug = %+v", noDebug)
	}
}

func TestParseJacocoMalformed(t *testing.T) {
	if _, err := ParseJacoco([]byte(`<report><package`)); err == nil {
		t.Fatal("malformed jacoco.xml must fail")
	}
}

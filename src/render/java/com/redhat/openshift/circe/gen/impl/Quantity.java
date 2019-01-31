package com.redhat.openshift.circe.gen.impl;

import com.redhat.openshift.circe.yaml.YamlProvider;

import java.lang.Math;

public class Quantity implements YamlProvider {

    public enum Unit {
        identity(1),
        Ki((int)Math.pow(1024,1)),
        Mi((int)Math.pow(1024,2)),
        Gi((int)Math.pow(1024,3)),
        Ti((int)Math.pow(1024,4)),
        Pi((int)Math.pow(1024,5)),
        Ei((int)Math.pow(1024,6)),
        k((int)Math.pow(1000,1)),
        M((int)Math.pow(1000,2)),
        m((int)Math.pow(1000,2)),
        G((int)Math.pow(1000,3)),
        T((int)Math.pow(1000,4)),
        P((int)Math.pow(1000,5)),
        E((int)Math.pow(1000,6)),
        ;

        private final long multiplier;

        Unit(long multiplier) {
            this.multiplier = multiplier;
        }
    }

    private final long count;
    private final Unit unit;
    private final long value;

    public Quantity(long count, Unit units) {
        this.count = count;
        this.unit = units;
        this.value = count * units.multiplier;
    }

    public Quantity(long count) {
        this(count, Unit.identity);
    }

    public String toYaml() {
        return ""+value;
    }

}

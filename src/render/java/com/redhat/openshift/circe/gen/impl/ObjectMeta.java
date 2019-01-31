package com.redhat.openshift.circe.gen.impl;

import java.util.Map;

public class ObjectMeta {

    private final String name, namespace;
    private final Map<String, Object> annotations, labels;

    public ObjectMeta(String namespace, String name, Map<String,Object> labels, Map<String,Object> annotations) {
        this.name = name;
        this.namespace = namespace;
        this.annotations = annotations;
        this.labels = labels;
    }

    public ObjectMeta(String namespace, String name, Map<String,Object> labels) {
        this(namespace, name, labels, null);
    }

    public ObjectMeta(String namespace, String name) {
        this(namespace, name, null, null);
    }

    public String getNamespace() {
        return this.namespace;
    }

    public String getName() {
        return this.name;
    }

    public Map<String,Object> getAnnotations() {
        return annotations;
    }

    public Map<String,Object> getLabels() {
        return labels;
    }

}

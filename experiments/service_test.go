package experiments

//func TestMultipleServicesForTemplateError(t *testing.T) {
//	templates := generateTemplates("bar")
//	templates[0].CreateService = true
//	e := newExperiment("foo", templates, "")
//	rs := templateToRS(e, templates[0], 1)
//	s1 := templateToService(e, templates[0], *rs)
//	s2 := templateToService(e, templates[0], *rs)
//
//	f := newFixture(t, e, rs, s1, s2)
//	defer f.Close()
//
//	f.run(getKey(e, t))
//
//}

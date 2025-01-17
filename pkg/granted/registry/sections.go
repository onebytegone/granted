package registry

import (
	"fmt"
	"regexp"
	"strings"

	"github.com/common-fate/clio"
	"github.com/common-fate/granted/pkg/cfaws"
	"gopkg.in/ini.v1"
)

func getGrantedGeneratedSections(config *ini.File) []*ini.Section {
	var grantedProfiles []*ini.Section

	isAutogeneratedSection := false
	for _, section := range config.Sections() {
		if section.Name() == ini.DefaultSection {
			continue
		}

		if strings.HasPrefix(section.Name(), "granted_registry_start") && !isAutogeneratedSection {
			isAutogeneratedSection = true
			grantedProfiles = append(grantedProfiles, section)

			continue
		}

		if strings.HasPrefix(section.Name(), "granted_registry_end") {
			isAutogeneratedSection = false
			grantedProfiles = append(grantedProfiles, section)

			continue
		}

		if isAutogeneratedSection {
			grantedProfiles = append(grantedProfiles, section)
		}
	}

	return grantedProfiles

}

func RemoveAutogeneratedProfileByRegistryURL(repoURL string) error {
	awsConfigFilepath, err := getDefaultAWSConfigLocation()
	if err != nil {
		return err
	}

	cFile, err := loadAWSConfigFile()
	if err != nil {
		return err
	}

	profiles := getGeneratedSectionByRegistryURL(cFile, repoURL)

	for _, p := range profiles {
		cFile.DeleteSection(p.Name())
	}

	return cFile.SaveTo(awsConfigFilepath)

}

func getGeneratedSectionByRegistryURL(config *ini.File, repoURL string) []*ini.Section {
	var profiles []*ini.Section

	isAutogeneratedSection := false
	for _, section := range config.Sections() {
		if section.Name() == ini.DefaultSection {
			continue
		}

		if strings.HasPrefix(section.Name(), ("granted_registry_start "+repoURL)) && !isAutogeneratedSection {
			isAutogeneratedSection = true
			profiles = append(profiles, section)

			continue
		}

		if strings.HasPrefix(section.Name(), ("granted_registry_end " + repoURL)) {
			isAutogeneratedSection = false
			profiles = append(profiles, section)

			continue
		}

		if isAutogeneratedSection {
			profiles = append(profiles, section)
		}
	}

	return profiles
}

func removeAutogeneratedProfiles(configFile *ini.File, awsConfigPath string) error {
	grantedProfiles := getGrantedGeneratedSections(configFile)
	// delete all autogenerated sections if any
	if len(grantedProfiles) > 1 {
		for _, gp := range grantedProfiles {
			configFile.DeleteSection(gp.Name())
		}

	}

	err := configFile.SaveTo(awsConfigPath)
	if err != nil {
		return err
	}

	return nil
}

// return all profiles that are not part of granted registry section.
func getNonGrantedProfiles(config *ini.File) []*ini.Section {
	isAutogeneratedSection := false
	var grantedProfiles []string
	for _, section := range config.Sections() {
		if strings.HasPrefix(section.Name(), "granted_registry_start") && !isAutogeneratedSection {
			isAutogeneratedSection = true
			grantedProfiles = append(grantedProfiles, section.Name())

			continue
		}

		if strings.HasPrefix(section.Name(), "granted_registry_end") {
			isAutogeneratedSection = false
			grantedProfiles = append(grantedProfiles, section.Name())

			continue
		}

		if isAutogeneratedSection {
			grantedProfiles = append(grantedProfiles, section.Name())
		}
	}

	var nonGrantedProfiles []*ini.Section
	for _, sec := range config.Sections() {
		if sec.Name() == ini.DefaultSection {
			continue
		}

		if !Contains(grantedProfiles, sec.Name()) {
			nonGrantedProfiles = append(nonGrantedProfiles, sec)
		}
	}

	return nonGrantedProfiles
}

func generateNewRegistrySection(configFile *ini.File, clonedFile *ini.File, repoURL string, isFirstSection bool) error {
	clio.Debugf("generating section %s", repoURL)
	err := configFile.NewSections(fmt.Sprintf("granted_registry_start %s", repoURL))
	if err != nil {
		return err
	}

	// add "do not edit" msg in the top of autogenerated code.
	if isFirstSection {
		configFile.Section(fmt.Sprintf("granted_registry_start %s", repoURL)).Comment = GetAutogeneratedTemplate()
	}

	currentProfiles := configFile.SectionStrings()
	namespace := formatURLToNamespace(repoURL)

	// iterate each profile section from clonned repo
	// add them to aws config file
	// if there is collision in the profile names then prefix with namespace.
	for _, sec := range clonedFile.Sections() {
		if sec.Name() == ini.DefaultSection {
			continue
		}

		// We only care about the non default sections for the credentials file (no profile prefix either)
		if cfaws.IsLegalProfileName(strings.TrimPrefix(sec.Name(), "profile ")) {

			if Contains(currentProfiles, sec.Name()) {
				clio.Debugf("profile name duplication found for %s. Prefixing %s to avoid collision.", sec.Name(), namespace)
				f, err := configFile.NewSection(appendNamespaceToDuplicateSections(sec.Name(), namespace))
				if err != nil {
					return err
				}

				*f = *sec
				if f.Comment == "" {
					f.Comment = "# profile name has been prefixed due to duplication"
				} else {
					f.Comment = "# profile name has been prefixed due to duplication. \n" + f.Comment
				}

				continue
			}

			f, err := configFile.NewSection(sec.Name())
			if err != nil {
				return err
			}

			*f = *sec
		}

	}

	err = configFile.NewSections(fmt.Sprintf("granted_registry_end %s", repoURL))
	if err != nil {
		return err
	}

	return nil
}

func Contains(arr []string, s string) bool {
	for _, v := range arr {
		if v == s {
			return true
		}
	}

	return false
}

func formatURLToNamespace(repoURL string) string {
	u, _ := parseGitURL(repoURL)

	namespaceArr := []string{u.Repo}
	if u.Subpath != "" {
		namespaceArr = append(namespaceArr, u.Subpath)
	}

	if u.Filename != "" {
		namespaceArr = append(namespaceArr, u.Filename)
	}

	return strings.Join(namespaceArr, "_")
}

func appendNamespaceToDuplicateSections(pName string, namespace string) string {
	regx := regexp.MustCompile(`(.*profile\s+)(?P<name>[^\n\r]*)`)

	if regx.MatchString(pName) {
		matches := regx.FindStringSubmatch(pName)
		nameIndex := regx.SubexpIndex("name")

		return "profile " + namespace + "." + matches[nameIndex]
	}

	return pName
}

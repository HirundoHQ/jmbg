package jmbg

// Region is the political/geographic region encoded in a JMBG.
type Region struct {
	Code    int
	Name    string
	Country string
}

// Country names used in the regions table.
const (
	countryForeign    = "foreign citizens"
	countryBosnia     = "Bosnia and Herzegovina"
	countryMontenegro = "Montenegro"
	countryCroatia    = "Croatia"
	countryMacedonia  = "Macedonia"
	countrySlovenia   = "Slovenia"
	countrySerbia     = "Serbia"
	countryVojvodina  = "Serbia/Vojvodina"
	countryKosovo     = "Serbia/Kosovo"
)

// regions maps region codes to Region values.
var regions = map[int]Region{
	// Foreign citizens
	0: {0, "naturalized citizens which had no republican citizenship", countryForeign},
	1: {1, "foreigners in Bosnia and Herzegovina", countryForeign},
	2: {2, "foreigners in Montenegro", countryForeign},
	3: {3, "foreigners in Croatia", countryForeign},
	4: {4, "foreigners in Macedonia", countryForeign},
	5: {5, "foreigners in Slovenia", countryForeign},
	6: {6, "foreigners in Serbia", countryForeign},
	7: {7, "foreigners in Serbia/Vojvodina", countryForeign},
	8: {8, "foreigners in Serbia/Kosovo", countryForeign},
	9: {9, "naturalized citizens which had no republican citizenship", countryForeign},
	// Bosnia and Herzegovina
	10: {10, "Banja Luka", countryBosnia},
	11: {11, "Bihać", countryBosnia},
	12: {12, "Doboj", countryBosnia},
	13: {13, "Goražde", countryBosnia},
	14: {14, "Livno", countryBosnia},
	15: {15, "Mostar", countryBosnia},
	16: {16, "Prijedor", countryBosnia},
	17: {17, "Sarajevo", countryBosnia},
	18: {18, "Tuzla", countryBosnia},
	19: {19, "Zenica", countryBosnia},
	// Montenegro
	21: {21, "Podgorica", countryMontenegro},
	22: {22, "Bar, Ulcinj", countryMontenegro},
	23: {23, "Budva, Kotor, Tivat", countryMontenegro},
	24: {24, "Herceg Novi", countryMontenegro},
	25: {25, "Cetinje", countryMontenegro},
	26: {26, "Nikšić", countryMontenegro},
	27: {27, "Berane, Rožaje, Plav, Andrijevica", countryMontenegro},
	28: {28, "Bijelo Polje, Mojkovac", countryMontenegro},
	29: {29, "Pljevlja, Žabljak", countryMontenegro},
	// Croatia
	30: {30, "Osijek, Slavonija", countryCroatia},
	31: {31, "Bjelovar, Virovitica, Koprivnica, Pakrac, Podravina", countryCroatia},
	32: {32, "Varaždin, Međimurje", countryCroatia},
	33: {33, "Zagreb", countryCroatia},
	34: {34, "Karlovac, Kordun", countryCroatia},
	35: {35, "Gospić, Lika", countryCroatia},
	36: {36, "Rijeka, Pula, Gorski kotar, Istra", countryCroatia},
	37: {37, "Sisak, Banovina", countryCroatia},
	38: {38, "Split, Zadar, Šibenik, Dubrovnik, Dalmacija", countryCroatia},
	39: {39, "Hrvatsko Zagorje", countryCroatia},
	// North Macedonia
	41: {41, "Bitola", countryMacedonia},
	42: {42, "Kumanovo", countryMacedonia},
	43: {43, "Ohrid", countryMacedonia},
	44: {44, "Prilep", countryMacedonia},
	45: {45, "Skopje", countryMacedonia},
	46: {46, "Strumica", countryMacedonia},
	47: {47, "Tetovo", countryMacedonia},
	48: {48, "Veles", countryMacedonia},
	49: {49, "Štip", countryMacedonia},
	// Slovenia
	50: {50, "Slovenia", countrySlovenia},
	// Serbia: evidencijski broj stranca (EBS). The Law on Foreigners, art. 7,
	// gives it the JMBG structure with register 06 for permanent residents and
	// asylees (already listed above) and register 66 for every other foreigner,
	// which is where temporary residence falls.
	66: {66, "foreigners in Serbia without permanent residence (EBS)", countryForeign},
	// Serbia
	70: {70, "citizens registered abroad at a diplomatic/consular post", countrySerbia},
	71: {71, "Belgrade", countrySerbia},
	72: {72, "Kragujevac", countrySerbia},
	73: {73, "Niš", countrySerbia},
	74: {74, "Leskovac, Vranje", countrySerbia},
	75: {75, "Zaječar, Bor", countrySerbia},
	76: {76, "Smederevo, Požarevac", countrySerbia},
	77: {77, "Mačva, Kolubara", countrySerbia},
	78: {78, "Čačak, Kraljevo, Kruševac", countrySerbia},
	79: {79, "Užice", countrySerbia},
	// Serbia / Vojvodina
	80: {80, "Novi Sad", countryVojvodina},
	81: {81, "Sombor", countryVojvodina},
	82: {82, "Subotica", countryVojvodina},
	83: {83, "Vrbas", countryVojvodina},
	84: {84, "Kikinda", countryVojvodina},
	85: {85, "Zrenjanin", countryVojvodina},
	86: {86, "Pančevo", countryVojvodina},
	87: {87, "Vršac", countryVojvodina},
	88: {88, "Ruma", countryVojvodina},
	89: {89, "Sremska Mitrovica", countryVojvodina},
	// Serbia / Kosovo
	91: {91, "Priština", countryKosovo},
	92: {92, "Kosovska Mitrovica", countryKosovo},
	93: {93, "Peć", countryKosovo},
	94: {94, "Đakovica", countryKosovo},
	95: {95, "Prizren", countryKosovo},
	96: {96, "Gnjilane, Kosovska Kamenica, Vitna, Novo Brdo", countryKosovo},
}

#!/usr/bin/env python3
###############################################################################
# Copyright (c) 2018 VMware, Inc.  All rights reserved. -- VMware Confidential
###############################################################################
# Scirpt functions as a wrapper around vmkperf for improved usability.

__author__ = 'VMware, Inc.'


##########
# imports
##########


import os
import sys
import glob
import time
import argparse
import shlex
import subprocess
import urllib.request
import ssl
import json
import collections
import textwrap
import signal
import multiprocessing
import threading


##############################
# module function definitions
##############################


# Prints error messages to stderr.
# @param: message - String to be sent to stderr
# @param: end - Last character of message string; defaults to newline character
def errPrint(message='', end='\n'):
    """Prints message to stderr."""

    sys.stderr.write('%s%s' % (message, end))
    sys.stderr.flush()

    return


# Handles SIGTERM and SIGINT signals sent to the program for a clean exit.
# @param: signal - Signal Number of Interest
# @param: frame - Interpreted Stack frame
# @throw: SignalInterupt - Function's sole intent is to raise the Exception
def signal_handler(signal, frame):
    """Throws an Exception when a terminating signal is sent to the program."""

    raise SignalInterupt

    return


# Formats the collected pcpu data from a specified event into csv.
# @param: eventComp - Components of a valid event
# @param: timeStamp - Timestamp of event's completed data collection
# @param: collectionNum - Collection Number
# @param: eventData - Raw event data from vmkperf poll
# @return: Formatted csv string of @params
def formatOutput(eventComp, timeStamp, collectionNum, eventData):
    """Formats the collected pcpu data to send to stdout."""

    # Splits the raw output of the vmkperf poll [event] command, by line, for
    # later processing.
    rawDataOutput = eventData.split('\n')

    # Isolates the pcpu data, then formats the data into a comma seperated
    # string (pcpu0, pcpu1, ..., pcpuN).
    pcpu = rawDataOutput[3].rstrip(' ').replace(' ', ',')

    # Determines what event "will be called" when the resulting csv string
    # is outputted to stdout. The priority for when an event "will be called"
    # is as follows:
    #           identifier -> name -> eventN
    name = eventComp[2] # identifier
    if name is None:
        name = eventComp[3] # name
    if name is None:
        name = eventComp[1] # eventN
    if eventComp[6] is not None:
        name = '%s-%s' % (eventComp[6], name)

    return '%s,%s,%s,%s' % (timeStamp, collectionNum, name, pcpu)


# Formats the compenents of a Core event to be used with vmkperf as:
#               vmkperf start [event].
# Event string is of the form:
#               [name] -e[Hex] -u[Hex] -d[Chars] -c[Hex]
# @param: eventComp - Components constructing a Core event
# @return: Formatted string of an event to be used as vmkperf start [event]
def formatCoreEvent(eventComp):
    """Formats Core event components for use as "vmkperf start [event]"."""

    eventN = eventComp[1] # Place in which the event was processed
    name = eventComp[3] # Name of the event
    eventSel = eventComp[4] # eventSel of the event
    unitMask = eventComp[5] # unitMask of the event
    modifiers = eventComp[6] # modifiers of the event
    cmask = eventComp[7] # cmask of the event

    # Determines which event components were provided with the event, and
    # constructs the event string accordingly.
    event = eventN
    if eventSel is None and unitMask is None:
        event = name
    if eventSel is not None:
        event = '%s -e%s' % (event, eventSel)
    if unitMask is not None:
        event = '%s -u%s' % (event, unitMask)
    if modifiers is not None:
        event = '%s -d%s' % (event, modifiers)
    if cmask is not None:
        event = '%s -c%s' % (event, cmask)

    return event


# Formats the compenents of an Uncore event to be used with vmkperf as:
#               vmkperf start [event].
# Event string is of the form:
#               [name] -e[Hex] -u[Hex] -d[Chars] -c[Hex]
# @param: eventComp - Components constructing an Uncore event
# @return: Formatted string of an event to be used as vmkperf start [event]
def formatUncoreEvent(eventComp):
    """Formats Uncore event components for use as "vmkperf start [event]"."""

    return None


# Formats the compenents of an Offcore event to be used with vmkperf as:
#               vmkperf start [event].
# Event string is of the form:
#               [name] -o [Hex - Request Mask]:[Hex - Response Mask]
# @param: eventComp - Components constructing an Offcore event
# @return: Formatted string of an event to be used as vmkperf start [event]
def formatOffcoreEvent(eventComp):
    """Formats Offcore event components for use as "vmkperf start [event]"."""

    return None


# Yields the next number from the [0, 1, ..., n-1], where n could infinity.
# @param: n - Number of integers in the list
# @return: Yields the next number in the list of [0, 1, ..., n-1]
def infinityGen(n=None):
    """Yields next number from [0, 1, ..., n-1], where n could be infinity."""

    i = 0   # Starting integer of the list

    # Terminating integer was provided. Hence, the generator will yield the
    # subsequent integer each time the generator is invoked, up to the
    # terminating integer, n-1
    if n is not None:
        while i < n:
            yield i
            i += 1

    # Terminating integer was NOT provided. The gerator will yield the
    # subsequent integer each time the generator is invoked.
    else:
        while True:
            yield i
            i += 1

    return


# Launches a process on the host to start a provided event in vmkperf.
# @param: event - Event to be invoked
# @return: TRUE if the event was started successfully; otherwise, FALSE
def tryStartEvent(event):
    """Attempts to start an event in vmkperf."""

    # Launches a process on the host to start an event with vmkperf. Stdout
    # and stderr is saved to verify the result of starting the event.
    cmd = 'vmkperf start %s' % (event)
    process = subprocess.Popen(shlex.split(cmd),
                               stdout=subprocess.PIPE,
                               stderr=subprocess.PIPE)
    out, err = process.communicate()
    process.terminate()

    # Successfully starting an event in vmkperf should send nothing to stdout
    # or stderr. Hence, if anything is printed out, then the attempt to start
    # an event has failed.
    if len(out) is not 0 or len(err) is not 0:
        return False

    return True


# Polls the requested PMU Event for a given interval.
# @param: event - PMU Event to be polled
# @param: n - The number of times the PMU Event was sampled
# @param: interval - Sampling interval of the PMU Event
def pollEvent(event, n, interval):
    """Samples a given PMU Event for a specified interval."""

    # Launches a process on the host to Poll the given PMU Event.
    cmd = ('vmkperf poll %s -i%s' % (event[1], interval))
    result = subprocess.Popen(shlex.split(cmd),
                            stdout=subprocess.PIPE,
                            stderr=subprocess.DEVNULL)
    stdout, _ = result.communicate()

    sys.stderr.write('pie')
    sys.stderr.flush()

    # Determines if the launched process terminated correctly. If not, then
    # polling the PMU Event fails and the function terminates.
    if result.returncode is not 1:
        return

    # Acquires the current time, in seconds, from the completed poll.
    timeStamp = int(time.time())

    # Format's the acquired PMU Event data from the pCPUs for command line
    # output, then prints it out.
    final = formatOutput(event,
                        timeStamp,
                        n,
                        stdout.decode('ascii'))
    print(final)

    # Flushes the stdout buffer.
    sys.stdout.flush()

    return



###########################
# module class definitions
###########################


# Handles command line arguments given to the script.
class ArgHandler(object):
    """Handles command line arguments given the script."""

    # Help message displayed when no command line arguments are given to the
    # script, indicating usage and command line option information.
    helpMessage = ('usage: ./perfcounters.py -[cuo] [event] -f [file] -i [time] -n [integer] -t [time] -l[+]\n\n'
                   '       Core Event (-c [event]) - e:[0xAA],u:[0xBB],d:[ukie],c:[0xCC]\n\n'
                   '                  eventSel => e:[0xAA]     format: 0xAA\n'
                   '                  unitMask => u:[0xBB]     format: 0xBB\n'
                   '                 modifiers => d:[ukei]\n'
                   '                                "u" (User)\n'
                   '                                "k" (Kernal)\n'
                   '                                "e" (Edge Detect)\n'
                   '                                "i" (Invert)\n'
                   '               counterMask => e:[0xCC]     format: 0xCC\n\n'
                   '     Uncore Event (-u [event]) - Currently Unsupported\n\n'
                   '    Offcore Event (-o [event]) - Currently Unsupported\n\n'
                   'Options:\n'
                   '----------\n'
                   '-c          | Core PMU Events\n'
                   '-u          | Uncore PMU Events\n'
                   '-o          | Offcore PMU Events\n'
                   '-f          | File input, containing a series of Core, Uncore, and Offcore Events\n'
                   '-i          | Sampling Interval                  format: [integer][SMH]\n'
                   '                                                         (Note: Time Unit indicator is OPTIONAL)\n'
                   '                                                         "S" - Seconds\n'
                   '                                                         "M" - Minutes\n'
                   '                                                         "H" - Hours\n'
                   '-t          | Amount of time to make Samples     format: [integer][SMH]\n'
                   '                                                         (Note: Time Unit indicator is OPTIONAL)\n'
                   '                                                         "S" - Seconds\n'
                   '                                                         "M" - Minutes\n'
                   '                                                         "H" - Hours\n'
                   '-n          | Number of Samples to Take          format: [integer]\n'
                   '-l          | Brief Descriptions of All PMU Events and Errata\n'
                   '-l+         | Detailed Information of ALL PMU Events\n')

    # Displays usage information when an incorrect command line option is
    # provided, or an options corresponding argument is not provided.
    usageMessage = ('./perfcounters.py -[cuo] [event] -f [file] -i [time]'
                    ' -n [integer] -t [time] -l[+]')

    # 1 second Sampling Interval for event PMU event data.
    defaultSampleInterval = 1

    # Infinite number of samples to collect from event PMU event data.
    defaultNumSamples = None

    # Time unit specifiers indicating the duration of a sampling interval,
    # or the amount of time allotted to collect event PMU event data.
    timeUnitSpecifiers = ['S', # Seconds
                          'M', # Minutes
                          'H'] # Hours

    def __init__(self):

        # Determines if any command line arguments were given. If none were
        # given, then the script's help message is displayed indicating how
        # to use the script.
        if len(sys.argv) is 1:
            print(ArgHandler.helpMessage)
            sys.stdout.flush()
            sys.exit(1)

        self._valid = False # Status of given command line arguments
        self._ignoreTime = False # Indicates if "-t" option should be ignored
        self.core = [] # Given Core Events
        self.uncore = [] # Given Uncore Events
        self.offcore = [] # Given Offcore Events
        self.interval = None # Event Sampling Interval
        self.numSamples = None # Number of Samples to take per Event
        self.time = None # Time allotted to collect Samples
        self.files = None # Files containing PMU events
        self.allEvents = [] # List of all core, uncore, and offcore events
        self.briefDescToggle = False # Brief Description Toggle
        self.detailedDescToggle = False # Detailed Description Toggle

        self._getArgs() # Acquires command line arguments

        # Processes all events given via the command line in the following
        # order: Core -> Uncore -> Offcore
        for event in self.core:
            self.allEvents.append(('cmd_line', 'core', event))
        for event in self.uncore:
            self.allEvents.append(('cmd_line', 'uncore', event))
        for event in self.offcore:
            self.allEvents.append(('cmd_line', 'offcore', event))

        # Immediately verifies the legitimacy of the provided command line
        # arguments.
        if self._verifyArgs() is True:
            self._valid = True


    # Indicates if provided commmand line arguments are valid for use.
    # @return: TRUE if valid command line arguments given; otherwise, FALSE
    def isValid(self):
        """Indicates if provided command line arguments are valid."""

        return self._valid


    # Determines if a provided string represents an integer.
    # @param: intVal - String representing an integer
    # @return: TRUE if string is an integer; otherwise, FALSE
    def _isValidInt(self, intVal):
        """Determines if a given string represents an integer value."""

        # Attempts to convert the string into an integer value; otherwise, an
        # exception is thrown, indicating that the string does not represent an
        # integer value.
        try:
            int(intVal, base=10)
        except (ValueError, TypeError):
            return False

        return True


    # Converts a string specifying time into seconds (an integer).
    # @param: timeUnit - String specifying a unit of time
    # @param: option - Specifies command line option the string is apart of
    # @return: Integer of specified time unit in seconds; otherwise, None
    def _convert2Sec(self, timeUnit, option):
        """Converts a string specifying time into seconds."""

        seconds = None # Resulting integer, in seconds
        unit = timeUnit[-1].upper() # Time unit being specified
        secPerMin = 60 # Seconds per Minute
        minPerHour = 60 # Minutes per Hour

        # Initially interprets given time in seconds. If attempt fails, then
        # attempts to interpret the given time as having a time unit. If this
        # attmpet fails, then an invalid time was given by the user.
        if self._isValidInt(timeUnit):
            time = int(timeUnit,
                       base=10)
            unit = None
        elif self._isValidInt(timeUnit[:-1]):
            time = int(timeUnit[:-1],
                       base=10)
            if unit not in ArgHandler.timeUnitSpecifiers:
                errPrint('Error: Invalid unit of time "%s" was specified for'
                         ' "%s" option.\n' % (unit, option))
                return seconds
        else:
            errPrint('Error: "%s" is not an integer.\n'
                     '       An integer was not given with the specified "%s" '
                     'unit of time for "%s" option.\n'
                     % (timeUnit[:-1], unit, option))
            return seconds

        # Determines which time unit was specified, then converts the time into
        # the seconds.
        if unit == "H":
            seconds = time * minPerHour * secPerMin
        elif unit == "M":
            seconds = time * secPerMin
        else:
            seconds = time

        return seconds


    # Parses a given file containing PMU events, storing parsed events by PMU.
    # The PMU type and Event must be seperated by a whitespace. Trailing and
    # leading whitespace is ignored. The file must be in the same directory as
    # this script.
    # @param: filename - Name of file containing PMU events
    def _parseFile(self, filename):
        """Parses file containing PMU events, storing parsed events by PMU."""

        # Attempts to open the given file. If the file cannot be opened, then
        # the function terminates normally.
        try:
            with open(filename) as content:

                # Processes each line in the given file.
                for line in content.read().splitlines():

                    # Attempts to split each line in the file by PMU type
                    # and Event. If there are more than, or less than, two
                    # groupings of characters, then it is immediately known
                    # that incorrect format was followed for an event in the
                    # file.
                    try:
                        pmu, event = line.split()
                    except ValueError:
                        errPrint('Error: Event "%s" in file "%s" could not be'
                                 ' parsed correctly.\n'
                                 '       Event will not be used.\n'
                                 % (line, filename))
                        continue

                    # Appends the provided event to its corresponding PMU;
                    # otherwise, an incorrect PMU type was given.
                    if pmu == '-c':
                        self.core.append(event)
                        self.allEvents.append((filename, 'core', event))
                    elif pmu == '-u':
                        self.uncore.append(event)
                        self.allEvents.append((filename, 'uncore', event))
                    elif pmu == '-o':
                        self.offcore.append(event)
                        self.allEvents.append((filename, 'offcore', event))
                    else:
                        errPrint('Error: "%s" of Event "%s" in file "%s" is '
                                 'not a valid PMU.\n'
                                 '       Event will not be used.\n'
                                 % (pmu, line, filename))

        except (FileNotFoundError, IOError):
            errPrint('Error: The file "%s" could not be opened.\n'
                     '       Events in the file cannot be processed.\n'
                     % (filename))

        return


    # Acquires command line arguments.
    def _getArgs(self):
        """Obtains command line arguments for interfacing with vmkperf."""

        # Establishes argument parser.
        parser = argparse.ArgumentParser(add_help=False,
                                         usage=ArgHandler.usageMessage)

        # Declares all command line options for this script.
        parser.add_argument('-c', # Core PMU Events
                            action='append',
                            dest='core',
                            required=False,
                            default=[]
                        )
        parser.add_argument('-u', # Uncore PMU Events
                            action='append',
                            dest='uncore',
                            required=False,
                            default=[]
                        )
        parser.add_argument('-o', # Offcore PMU Events
                            action='append',
                            dest='offcore',
                            required=False,
                            default=[]
                        )
        parser.add_argument('-i', # Sampling interval
                            action='append',
                            dest='interval',
                            required=False,
                            default=[]
                        )
        parser.add_argument('-n', # Number of Samples to take
                            action='append',
                            dest='numSamples',
                            required=False,
                            default=[]
                        )
        parser.add_argument('-t', # Time in which to collect samples
                            action='append',
                            dest='time',
                            required=False,
                            default=[]
                        )
        parser.add_argument('-f', # File containing PMU events
                            action='append',
                            dest='files',
                            required=False,
                            default=[]
                        )
        parser.add_argument('-l', # Bried Desctiption Toggle
                            action='store_true',
                            dest='briefDescriptionToggle',
                            required=False,
                            default=False
                            )
        parser.add_argument('-l+', # Detailed Description Toggle
                            action='store_true',
                            dest='detailedDescriptionToggle',
                            required=False,
                            default=False
                            )

        argList = parser.parse_args() # Parses command line options

        # Stores command line arguments in instance variables.
        self.core = argList.core.copy()
        self.uncore = argList.uncore.copy()
        self.offcore = argList.offcore.copy()
        self.interval = argList.interval.copy()
        self.numSamples = argList.numSamples.copy()
        self.time = argList.time.copy()
        self.files = argList.files.copy()
        self.briefDescToggle = argList.briefDescriptionToggle
        self.detailedDescToggle = argList.detailedDescriptionToggle

        return


    # Verifies the validity of all provided command line arguments.
    # @return: TRUE if all arguments are valid; otherwise, FALSE
    def _verifyArgs(self):
        """Validates all command line arguments given."""

        # Verifies if "-l" and "-l+" options were given together. If so, only
        # the "-l+" option is valid for use.
        if self.briefDescToggle is True and self.detailedDescToggle is True:
            errPrint('Warning: "-l" and "-l+" options cannot be used '
                     'together\n'
                     '         Detailed descriptions will be '
                     'displayed (-l+).\n')
            self.briefDescToggle = False

        # Verifies if any other arguments were given alongside the "-l" and/or
        # "-l+" options. If so, all other command line arguments are ignored.
        if (len(self.core) is not 0 or len(self.uncore) is not 0 or
            len(self.offcore) is not 0 or len(self.interval) is not 0 or
            len(self.numSamples) is not 0 or len(self.time) is not 0 or
            len(self.files) is not 0):

            # Indicates that all other command line arguments will be ignored.
            if self.briefDescToggle is True or self.detailedDescToggle is True:
                if self.briefDescToggle is True:
                    errPrint('Warning: Once "-l" option is invoked, all other '
                             'command line\n'
                             '         arguments are ignored.\n')
                if self.detailedDescToggle is True:
                    errPrint('Warning: Once "-l+" option is invoked, all other'
                             ' command line\n'
                             '         arguments are ignored.\n')

        # Determines if multiple arguments were given to the "-i", "-n", or
        # "-t" options. If so, then only the first argument given will be used.
        for option in ['-i', '-n', '-t']:
            # Locates the corresponding list of command line arguments.
            if option == '-i':
                optionArgs = self.interval
            elif option == '-n':
                optionArgs = self.numSamples
            else:
                optionArgs = self.time

            # Determines if multiple arguments were given for a command line
            # option.
            if len(optionArgs) > 1:
                errPrint('Warning: Multiple arguments given to the "%s" '
                         'option.\n'
                         '         Arguments:\n' % (option))
                message = '                   %s <-- First Argument\n'
                for arg in optionArgs:
                    errPrint(message % arg)
                    message = '                   %s\n'
                errPrint('         Only the first argument will be used.\n')

        # Determines if a valid unit of time was specified for the sampling
        # interval, "-i" option.
        if len(self.interval) is 0:
            # Default sampling interval is used if interval not specified.
            self.interval = ArgHandler.defaultSampleInterval
        else:
            # Attempts to convert the specified time into seconds. If None
            # is returned, then an invalid time was given.
            self.interval = self._convert2Sec(self.interval[0], '-i')
            if self.interval is None:
                return False

        # Determines if a time to collect samples AND a specified number of
        # samples were given. If so, then the "-t" option will be ignored.
        if len(self.numSamples) > 0 and len(self.time) > 0:
            errPrint('Warning: "-n" and "-t" options cannot be used '
                     'simultaneously.\n'
                     '         "-t" option will be ignored.\n')
            self._ignoreTime = True

        # Determines if a valid number of samples, "-n" option, was specified.
        if len(self.numSamples) is 0:
            # Default number of samples will be taken, which is infinite.
            self.numSamples = ArgHandler.defaultNumSamples
        else:
            # Verifies that an integer value was given.
            try:
                self.numSamples = int(self.numSamples[0], base=10)
            except (ValueError, TypeError):
                errPrint('Error: "%s" is not an integer.\n'
                         '       "-n" option requires an integer value.\n'
                          % (self.numSamples[0]))
                return False

        # Determines if a valid unit of time was specified for data collection,
        # "-t" option.
        if self._ignoreTime is True or len(self.time) is 0:
            # Indicates that "-t" option will not be used.
            self.time = None
        else:
            # Attempts to convert the specified time into seconds. If None
            # is returned, then an invalid time was given.
            self.time = self._convert2Sec(self.time[0], '-t')
            if self.time is None:
                return False

        # Determines if valid event files were specified for use. If so, then
        # the events in each of the provided files are parsed and categorized
        # by PMU event types.
        if len(self.files) is 0:
            # Indicates that "-f" option will not be used.
            self.files = None
        else:
            # Attempts to parse the events provided in each of the event files.
            for filename in self.files:
                self._parseFile(filename)

        return True


# Handles acquiring and parsing Intel JSON files for architecture's PMU events.
class ArchitecturePMU(object):
    """Handles acquiring and parsing of Intel JSON PMU event files"""

    # URL for ALL Intel CPU Architecture JSON files.
    url = 'https://download.01.org/perfmon/'

    # CSV file from the provided URL necessary for mapping the server's
    # CPU architecture to the correct Intel JSON files containing the
    # architecture's PMU events.
    mapfile = 'mapfile.csv'

    # Folder containing the "mapfile" and all Intel JSON files (Core,
    # Uncore, and Offcore) of PMU events.
    pmuEventFolder = "architecture-pmu-JSON"

    def __init__(self):

        # Creates a folder in /tmp/ directory for storing the Intel
        # JSON files, then changes directoary into the newly created
        # folder to function as the present working directory.
        os.chdir('/tmp/')
        if not os.path.isdir(ArchitecturePMU.pmuEventFolder):
            os.mkdir(ArchitecturePMU.pmuEventFolder)
        os.chdir(ArchitecturePMU.pmuEventFolder)

        self._cpuFamily = 'N/A' # CPU Family of the Server
        self._cpuModel = 'N/A' # CPU Model of the Server
        self._cpuType = 'N/A' # CPU Type Specifier
        self._cpuStepping = 'N/A' # CPU Stepping Level of the Server
        self._cpuVersion = 'N/A' # CPU Version of the Server
        self._cpuName = 'N/A' # CPU Name Abbreviation
        self._cpustr = 'N/A' # Identifies CPU in the "mapfile"
        self.JSONFiles = [] # List of Intel JSON files
        self.core = collections.OrderedDict() # Core Events
        self.unusedCore = collections.OrderedDict() # Unused Core Events
        self.uncore = collections.OrderedDict() # Uncore Events
        self.unusedUncore = collections.OrderedDict() # Unused Uncore Events
        self.offcore = collections.OrderedDict() # Offcore Events
        self.unusedOffcore = collections.OrderedDict() # Unused Offcore Events
        self.vmkperfEvents = {} # vmkperf recognizable events
        self._validParse = False # Indicates all JSON files are parsed

        self._parseVMKEvents() # Parses vmkperf events

        # Identifies the CPU Architecture present on the server, then downloads
        # the "mapfile", which indiate which Intel JSON files to download based
        # off of the architecture. The Intel JSON files indicate the
        # architecture's Core, Uncore, and Offcore events (3 JSON files). Once
        # downloaded, the files are parsed and processed for use.
        self._verifyArchitecture()
        if self._cpustr != 'N/A':
            if self._getJSONFiles() is True:
                # Ensures that the "matrix" JSON file for Offcore Events is
                # parsed first. Offcore Event info is split between "matrix"
                # and "core" JSON files.
                for index, aFile in enumerate(self.JSONFiles):
                    if aFile.find('matrix') is -1:
                        del self.JSONFiles[index]
                        self.JSONFiles.append(aFile)
                self._parseJSONFiles()

        os.chdir('/tmp/') # Returns to /tmp/ directory


    # Indicates if the Core, Uncore, and Offcore JSON files have been parsed.
    def isValidParse(self):
        """Indicates if Core/Uncore/Offcore JSON files have been parsed."""

        return self._validParse


    # Identifies the CPU Architecture of the server based on Family and Model.
    def _verifyArchitecture(self):
        """Identifies the CPU Architecture of the server by Family-Model."""

        # Starts a process which executes a shell command to acquire
        # CPU Architecture Info.
        cmd = 'vsish -e get /hardware/cpu/cpuList/0/'
        process = subprocess.Popen(shlex.split(cmd),
                                   stdout=subprocess.PIPE)
        stdout, _ = process.communicate()
        process.terminate()

        # Parses the output from the shell command to acquire CPU
        # info: Family, Model, Type, Stepping
        for line in stdout.decode('ascii').split():
            if line.startswith('Family:'):
                self._cpuFamily = line.replace('Family:', '')
                continue
            if line.startswith('Model:'):
                self._cpuModel = line.replace('Model:', '')
                continue
            if line.startswith('Type:'):
                self._cpuType = line.replace('Type:', '')
                continue
            if line.startswith('Stepping:'):
                self._cpuStepping = line.replace('Stepping:', '')
                break

        # Determines if the CPU Family AND Model were acquired. If so, then
        # PMU Event JSON files can be found from the constructed CPU
        # string.
        if self._cpuFamily == 'N/A' or self._cpuModel == 'N/A':
            errPrint('Error: Unable to verify CPU Family and/or Model\n')
        else:
            self._cpustr = ('GenuineIntel-%s-%s'
                            % (int(self._cpuFamily, base=16),
                               self._cpuModel[-2:].upper()))

        return


    # Downloads a file over HTTPS.
    # @param: webFile - Name of file from specified HTTPS connection
    # @param: destFile - Name to save downloaded file as
    # @return: TRUE if download is successful; otherwise, FALSE
    def _download(self, webFile, destFile):
        """Downloads a file over HTTPS."""

        # Starts a process to execute a shell command to view the
        # network firewall settings of the server.
        cmd = 'esxcli network firewall ruleset list'
        process = subprocess.Popen(shlex.split(cmd),
                                stdout=subprocess.PIPE)
        stdout, _ = process.communicate()

        # Attempts to locate the current status of the httpCLient
        # on the server, and remembers it.
        httpClientstate = False # Enabled status of the server's httpClient
        for line in stdout.decode("ascii").split("\n"):
            # Attempts to split each lien by the name of the ruleset and
            # the enabled status.
            try:
                name, enabled = line.split()
                if name == "httpClient":
                    # Remembers the servers present HTTPClient enabled status
                    httpClientstate = True if enabled == "true" else False
                    break
            except ValueError:
                pass
        else:
            errPrint('Error: Unable to verify the current httpClient enabled'
                     ' status. Could you have disabled the firewall module on' 
                     ' ESX?\n')
            return False

        # Starts a process to enable the httpClient on the server.
        cmd = ('esxcli network firewall ruleset set --enabled=True '
               '--ruleset-id=httpClient')
        process = subprocess.run(shlex.split(cmd))

        url = os.path.join(ArchitecturePMU.url, webFile) # url of desired file

        # Disables SSL certicate verification when downloading files from an
        # https client.
        context = ssl._create_unverified_context()

        # Attempts to download the disired file and saves it in the specified
        # file.
        try:
            with urllib.request.urlopen(url,
                                        context=context,
                                        timeout=5) as response:
                with open(destFile, 'wb') as out_file:
                    data = response.read()
                    out_file.write(data)
        except (OSError, urllib.request.URLError, FileNotFoundError, IOError):
            errPrint('Error: Unable to download file "%s" from\n'
                     '       "%s"\n.'
                     % (webFile, url))
            return False

        # Verifies that the renamed downloaded file is in the present working
        # directory.
        if not os.path.isfile(destFile):
            errPrint('Error: Unable to locate file "%s" in\n'
                     '       "%s"\n'
                     % (destFile, os.getcwd()))
            return False

        # Disables the httpClient on the server if the server's status prior to
        # downloading the file was disabled.
        if httpClientstate is False:
            cmd = ('esxcli network firewall ruleset set --enabled=False'
                   ' --ruleset-id=httpClient')
            process = subprocess.run(shlex.split(cmd))

        return True


    # Acquires Intel JSON files of CPU architecture PMU Events.
    # @return: TRUE if all files were obtained; otherwise, FALSE
    def _getJSONFiles(self):
        """Acquires Intel JSON files of CPU architecture PMU Events."""

        # Downloads the "mapfile" for determining which Intel JSON files are
        # to be downloaded.
        if not os.path.isfile(ArchitecturePMU.mapfile):
            # Verifies that "mapfile" was downloaded.
            if not self._download(webFile=ArchitecturePMU.mapfile,
                                  destFile=ArchitecturePMU.mapfile):
                return False

        # Opens the "mapfile", and determines which Intel JSON files are to be
        # downloaded based on the CPU architecture.
        foundJSONFiles = [] # JSON files identified in the "mapfile"
        uniqueFoundJSON = {'core' : None, # Indicate sunique PMU Event Files
                           'uncore' : None,
                           'offcore' : None}
        with open(ArchitecturePMU.mapfile) as f:
            for line in f:
                try:
                    # Parses each line in the "mapfile" to locate the Intel
                    # JSON files by CPU Family-Model, CPU version, filename,
                    # and type of PMU Event.
                    line = line.strip('\n').split(',') # Values in each line
                    cpuType, version, filename, eventType = line
                    if cpuType == self._cpustr:
                        # Verifies if a previous JSON file had been specified
                        # for Core, Uncore, or Offcore PMU Events.
                        if uniqueFoundJSON[eventType] is None:
                            uniqueFoundJSON[eventType] = filename
                            foundJSONFiles.append(filename[1:])
                            self._cpuVersion = version
                            self._cpuName = (filename.split('/'))[1]
                        else:
                            errPrint('Error: Duplicate "%s" JSON file '
                                     'detected while parsing "%s".\n'
                                     '       "%s" will not downloaded\n'
                                     '       from: "%s".\n'
                                     % (eventType,
                                        ArchitecturePMU.mapfile,
                                        filename,
                                        ArchitecturePMU.url))
                except (ValueError,KeyError):
                    pass

            # Ensures that the Core, Uncore, and Offcore JSON files have
            # been downloaded.
            if len(foundJSONFiles) is not 3:
                errPrint('Error: Expected 3 JSON files for Core, Uncore, and '
                         '       Offcore PMU Events.\n'
                         '       JSON files recieved:\n')
                for i in foundJSONFiles:
                    errPrint('          - "%s"' % (i))
                return False

        # Searches for the Core, Uncore, and Offcore JSON files. If they are
        # not found, then each JSON file is downloaded.
        for webFile in foundJSONFiles:
            # Constructs a new filename for the JSON file, based off of the
            # original filename.
            _, pmuType, version = webFile.split('_')
            JSONFilename = '%s_%s_%s' % (self._cpustr, pmuType, version)
            # Attempts to locate the JSON filename; otherwise, the JSON file
            # is downloaded
            if not os.path.isfile(JSONFilename):
                # Verifies that the requested file was downloaded.
                if not self._download(webFile=webFile,
                                      destFile=JSONFilename):
                    return False
                else:
                    self.JSONFiles.append(JSONFilename)
            else:
                self.JSONFiles.append(JSONFilename)

        return True


    # Parses and stores all vmkperf recognizable PMU Events.
    def _parseVMKEvents(self):
        """Stores all vmkperf recognizable events in a dictionary."""

        # Starts a process on the server to get a list of vmkperf events.
        cmd = "vmkperf listevents"
        out = subprocess.Popen(shlex.split(cmd),
                               stdout=subprocess.PIPE)
        stdout, _ = out.communicate()
        out.terminate()
        eventList = stdout.decode("ascii").split("\n")

        # Processes each line of vmkperf listed events
        for line in eventList:
            # Tries to parse each line into 4 components. Otherwise,
            # the line does not represent an event.
            try:
                # Attempts to parse the line into the 3 corresponding
                # components: Event Name, eventSel, and unitMask
                name, e, u, _ = line.split()

                # Processes eventSel
                eventSel = e[9:]
                if eventSel is '0':
                    eventSel = None
                elif len(eventSel) is 3:
                    eventSel = "0x0{0}".format(eventSel[-1])
                else:
                    pass

                # Processes unitMask
                unitMask = u[9:]
                if unitMask is '0':
                    unitMask = None
                elif len(unitMask) is 3:
                    unitMask = "0x0{0}".format(unitMask[-1])
                else:
                    pass

                eventDesc = "vmkperf recognizable event: Description Unavailable"

                # Maps Event Name -> eventSel & unitMask
                self.vmkperfEvents[name] = ((eventSel, unitMask), eventDesc)

                # Maps eventSel & unitMask -> Event Name
                if eventSel is not None or unitMask is not None:
                    self.vmkperfEvents[(eventSel, unitMask)] = (name, eventDesc)

            except ValueError:
                pass

        return


    # Parses Core PMU Events from the "core" JSON File.
    # @param: data - List of dictionaries from "core" JSON file
    def _parseCoreJSONFile(self, data):
        """Parses the Core PMU Events from a JSON file."""

        # Goes through all events present in the "core" JSON file.
        for event in data:
            # Data from the "core" JSON file also contains Offcore PMU Events.
            # Offcore PMU events are ignored.
            if event['Offcore'] == '1':
                pass
            else:
                # Hash Key of a Core Event's name
                name = event['EventName']

                # Hash Key of a Core Event's main components
                maskKey = (event['EventCode'],
                           event['UMask'],
                           event['EdgeDetect'],
                           event['Invert'],
                           event['AnyThread'],
                           event['CounterMask'])

                # Creates a hash table between a Core PMU Event name, and
                # Event Components, and the dictionary containing all of the
                # associated information about the event.
                if len(event['EventCode']) is 4: # Ignores invalid EventCodes

                    # Ignores any Event which requires toggling of the
                    # "Any Thread" bit.
                    if event['AnyThread'] == '1':
                        self.unusedCore[name] = event
                        self.unusedCore.move_to_end(name,
                                                    last=True)
                        if maskKey not in self.unusedCore:
                            self.unusedCore[maskKey] = [event]
                        else:
                            self.unusedCore[maskKey].append(event)

                    # Creates hash table of Event name/components that maps to
                    # Event information.
                    elif maskKey not in self.core:
                        self.core[maskKey] = event
                        self.core.move_to_end(maskKey,
                                              last=True)
                        self.core[name] = event
                        self.core.move_to_end(name,
                                              last=True)

                    # Event cannot be used since it's components cause a
                    # collision in the Core Events hash table. These events are
                    # stored in a separate dictionary of unused events, which
                    # handles all of the collisions by creating a list of all
                    # the colliding events based on the event's components.
                    else:
                        self.unusedCore[name] = event
                        self.unusedCore.move_to_end(name,
                                                    last=True)
                        if maskKey not in self.unusedCore:
                            self.unusedCore[maskKey] = [event]
                        else:
                            self.unusedCore[maskKey].append(event)

                # Stores invalid EventCodes in an unused event dictionary.
                # Invalid EventCodes implies a Core Event with multiple
                # EventCodes, similar to an Offcore Event.
                else:
                    self.unusedCore[name] = event
                    self.unusedCore.move_to_end(name,
                                                last=True)
                    if maskKey not in self.unusedCore:
                            self.unusedCore[maskKey] = [event]
                    else:
                        self.unusedCore[maskKey].append(event)

        return


    # Parses Uncore PMU Events from the "uncore" JSON File.
    # @param: data - List of dictionaries from "uncore" JSON file
    def _parseUncoreJSONFile(self, data):
        """Parses the Uncore PMU Events from a JSON file."""

        return None


    # Parses Offcore PMU Events from the "core" JSON File.
    # Necessary Offcore PMU Event information is stored in the "offcore" JSON
    # file, though the event names are stored in the "core" JSON file.
    # @param: data - List of dictionaries from "core" JSON file
    # @param: matrix - List of dictionaries from "offcore" JSON file
    def _parseOffcoreJSONFile(self, data, matrix):
        """Parses the Offcore PMU Events from a JSON file."""

        return None


    # Parses all Core, Uncore, and Offcore JSON files.
    def _parseJSONFiles(self):
        """Parses all Core, Uncore, and Offcore JSON files."""

        matrixData = None # Additional Offcore Event info

        # Processes all JSON files.
        for pmuFile in self.JSONFiles:
            try:
                with open(pmuFile) as data:
                    pmuRawData = json.load(data)

                    # Offcore JSON file cannot be processed unless the
                    # additional information from the offcore "matrix" JSON
                    # file is processed first.
                    if pmuFile.find('matrix') is not -1:
                        matrixData = pmuRawData
                        continue

                    # Parses Uncore PMU Events
                    if 'Unit' in pmuRawData[0]:
                        self._parseUncoreJSONFile(pmuRawData)

                    # Parses Core and Uncore PMU Events
                    else:
                        self._parseOffcoreJSONFile(pmuRawData, matrixData)
                        self._parseCoreJSONFile(pmuRawData)

            # JSON file couldn't be opened.
            except (FileNotFoundError, IOError):
                errPrint('Error: Unable to open "%s".\n' % (pmuFile))
                self._validParse = False
                return

        self._validParse = True # All JSON files were parsed correctly

        return


    # Displays information regarding architecture's available PMU Events.
    # @param: brief - Displays BRIEF event info if TRUE; otherwise, DETAILED
    def displayDesciption(self, brief=True):
        """Displays info about all architecture PMU Events."""

        # Header Info
        print('-' * 80)
        print(' ' * 10, end='')
        print('PMU Event Descriptions for Core, Uncore, and Offcore Events')
        print('-' * 80, end='\n\n')
        print('- Intel CPU Architecture "%s" Info:\n' % (self._cpuName))
        print('  Family: %s' % (self._cpuFamily))
        print('  Model: %s' % (self._cpuModel))
        print('  Version: %s' % (self._cpuVersion))
        print('  Type: %s' % (self._cpuType))
        print('  Stepping: %s' % (self._cpuStepping))
        print('  JSON Identifier: %s\n' % (self._cpustr))
        print('- PMUs corresponding to this host\'s architecture are '
              'displayed below.\n')
        print('- Type of PMU: Core, Uncore, Offcore (Displayed in this '
              'order)\n')
        print('- For detailed information about PMU Event Errata, locate '
              'Intel\'s Specification\n'
              '  Update Manual using the provided Architecture information.\n')
        print('-' * 80, end='\n\n')

        # Verifies that all JSON files have been parsed; otherwise, PMU Event
        # info cannot be displayd.
        if self._validParse is True:
            # Determines if "brief" PMU Event info should be displayed.
            if brief is True:
                self.displayBriefCoreInfo()
                print()
                self.displayBriefUncoreInfo()
                print()
                self.displayBriefOffcoreInfo()
            # Displays detailed PMU Event info.
            else:
                self.displayDetailCoreInfo()
                print()
                self.displayDetailUncoreInfo()
                print()
                self.displayDetailOffcoreInfo()

        # If the Core, Uncore, or Offcore JSON files have not all been
        # processed, then no information is displayed.
        else:
            print('Unable to download / locate / parse Architecture PMU Events '
                  'from JSON files.')
        sys.stdout.flush()

        return


    # Displays brief information about Core PMU Events.
    def displayBriefCoreInfo(self):
        """Displays brief information about Core PMU Events."""

        # Establishes Header information about each Core Event.
        print('PMU Core Events:\n')
        # Formatting template when displaying information
        template = '%-28s  %-18s  %-30s'
        print(template % ('Event Name',
                          'Errata',
                          'Brief Description'))
        print('-' * 80)

        # Displays info about all known Core Events from the JSON files
        # (Supported and Unsupported).
        for pmuEvents in [self.core, self.unusedCore]:
            for event in pmuEvents:

                # Disregards all hash table keys that are not Core Event names.
                if not isinstance(event, tuple):

                    # Breaks down all Event information into fixed length
                    # strings for formatted output.
                    nameMessage = event # Name of the Event
                    # Appends an "Unsupported" indicator onto the Event's name
                    # if the Event is stored in the unused Core Events
                    # dictionary.
                    if pmuEvents is self.unusedCore:
                        nameMessage = ('%s %s'
                                       % (event, '(Currently Unsupported)'))
                    name = textwrap.wrap(nameMessage,
                                        width=28)
                    errata = textwrap.wrap(pmuEvents[event]['Errata'],
                                            width=18)
                    desc = textwrap.wrap(pmuEvents[event]['BriefDescription'],
                                        width=30)

                    # Prints all information for a single Event, line by line,
                    # until no more information is available.
                    for i in range(0, max(len(name),
                                        len(desc),
                                        len(errata))):
                        namePiece = ''
                        errataPiece = ''
                        descPiece = ''
                        if i < len(name):
                            namePiece = name[i]
                        if i < len(errata):
                            errataPiece = errata[i]
                        if i < len(desc):
                            descPiece = desc[i]
                        formatTuple = (namePiece,
                                    errataPiece,
                                    descPiece)
                        print(template % formatTuple)
                    print()

        return


    # Displays brief information about Uncore PMU Events.
    def displayBriefUncoreInfo(self):
        """"""

        print('PMU Uncore Events:\n')
        print('    Currently Unsupported.')

        return


    # Displays brief information about Offcore PMU Events.
    def displayBriefOffcoreInfo(self):
        """"""

        print('PMU Offcore Events:\n')
        print('    Currently Unsupported.')

        return


    # Displays ALL known information about Core PMU Events.
    def displayDetailCoreInfo(self):
        """Displays ALL known information about Core PMU Events."""

        # Establishes Header information about each Core PMU Event.
        print('PMU Core Events:\n')

        randEvent = list(self.core.keys())[0]
        mainHeaders = ['EventName', 'Errata', 'PublicDescription', 'EventCode', 'UMask']
        headerKeys = list(set(self.core[randEvent].keys()) - set(mainHeaders) - set(['BriefDescription']))
        allHeaders = tuple(mainHeaders + headerKeys)
        lengthKeys = [28, 18, 30, 8, 8]

        template = ('%s%is  %s%is  %s%is  %s%is  %s%is'
                    % ('%', -lengthKeys[0],
                       '%', -lengthKeys[1],
                       '%', -lengthKeys[2],
                       '%', lengthKeys[3],
                       '%', lengthKeys[4]))
        for header in headerKeys:
            newLength = len(self.core[randEvent][header])
            template = '%s  %s%ss' % (template, '%', newLength)
            lengthKeys.append(newLength)
        headers = template % allHeaders
        print(headers)
        print('-' * len(headers))

        # Displays info about all known Core Events from the JSON files
        # (Supported and Unsupported).
        for pmuEvents in [self.core, self.unusedCore]:
            for event in pmuEvents:
                if not isinstance(event, tuple):

                    # Breaks down all Event information into fixed length
                    # strings for formatted output.
                    nameMessage = event # Name of the Event
                    # Appends an "Unsupported" indicator onto the Event's name
                    # if the Event is stored in the unused Core Events
                    # dictionary.
                    if pmuEvents is self.unusedCore:
                        nameMessage = ('%s %s'
                                       % (event, '(Currently Unsupported)'))

                    allEventLines = []
                    for i, j in zip(allHeaders, lengthKeys):
                        # allEventLines.append(textwrap.wrap(pmuEvents[event][i],
                        #                      width=j))
                        allEventLines.append(pmuEvents[event][i])
                    # allEventLines[0] = textwrap.wrap(nameMessage,
                    #                                  width=lengthKeys[0])
                    allEventLines[0] = nameMessage

                    print(template % tuple(allEventLines))
                print()

        return


    # Displays ALL known information about Uncore PMU Events.
    def displayDetailUncoreInfo(self):
        """Displays ALL known information about Uncore PMU Events."""

        print('PMU Uncore Events:\n')
        print('    Currently Unsupported.')

        return


    # Displays ALL known information about Offcore PMU Events.
    def displayDetailOffcoreInfo(self):
        """Displays ALL known informatiob about Offcore PMU Events."""

        print('PMU Offcore Events:\n')
        print('    Currently Unsupported.')

        return

#
# NOTE: The documentation for this class is sparse, and I do apologize.
#

# Handles processing all PMU Events for use with vmkperf.
class ProcessPMUEvent(object):
    """Handles processing of all PMUE Events for use with vmkperf."""

    # Core Event Flags used to invoke an event.
    _eventFlags = { "i" : None,      # Identifier
                    "e" : None,      # eventSel
                    "u" : None,      # unitMask
                    "d" : None,      # modifiers
                    "c" : None       # cmask
                  }

    # Available Core Event modifiers
    _eventFlagModifiers = { "u" : False,       # User
                            "k" : False,       # Kernal
                            "e" : False,       # Edge
                            "i" : False        # Invert
                          }

    def __init__(self, pmuInfo):
        self.usedEvents = {} # All used Events
        self.usedIdentifiers = {} # All used Event Identifiers
        self.coreJSON = pmuInfo.core # Valid Core Events
        self.unusedCoreJSON = pmuInfo.unusedCore # Unused Core Events
        self.coreVmkperf = pmuInfo.vmkperfEvents # vmkperf Events
        self.uncoreJSON = pmuInfo.uncore # Valid Uncore Events
        self.unusedUncoreJSON = pmuInfo.unusedUncore # Unused Uncore Events
        self.offcoreJSON = pmuInfo.offcore # Valid Offcore Events
        self.unusedOffcoreJSON = pmuInfo.unusedOffcore # Unused Offcore Events
        self.numProcessed = 0 # Number of processed Events


    def processEvent(self, event):
        """Parses the provided vmkperf event into the corresponding compenents."""

        pmu = event[1]
        result = None

        if pmu == "core":
            result = self._processCoreEvent(event[2], event[0])
        elif pmu == "uncore":
            eventComp = self._processUncoreEvent(event[2], event[0])
            if eventComp is not None:
                result = self._verifyUncoreEvent(eventComp, event[2], event[0])
        elif pmu == "offcore":
            eventComp = self._processOffcoreEvent(event[2], event[0])
            if eventComp is not None:
                result = self._verifyOffcoreEvent(eventComp, event[2], event[0])
        else:
            pass
        self.numProcessed += 1

        return result


    def _isValidHex(self, hexVal):
        """Determines if a given string represents a valid 1-byte Hexadecimal
        value of the form '0xAA'."""

        #
        # Attempts to convert the string into a Hexadecimal value; otherwise, an
        # exception is thrown, indicating that the string does not represent a
        # hexadecimal value. Also, if the provided Hexadecimal value is not
        # represented using 1-byte, the value of incorrect format for vmkperf.
        #
        try:
            int(hexVal, base=16)
        except (ValueError, TypeError):
            return False
        finally:
            if len(hexVal) is not 4:
                return False

        return True


    def _verifyCoreEvent(self, eventComp, event, origin):
        """"""

        #
        # All necessary components of an event.
        #
        eventN = eventComp[1]
        identifier = eventComp[2]
        name = eventComp[3]
        eventSel = eventComp[4]
        unitMask = eventComp[5]
        modifiers = eventComp[6]
        cmask = eventComp[7]

        if eventSel is not None:
            eventSel = eventSel[0:2]+eventSel[2:].upper()

        if unitMask is not None:
            unitMask = unitMask[0:2]+unitMask[-2:].upper()

        if cmask is None:
            cmask = '0'
        else:
            cmask = str(int(eventComp[7], base=16))

        userMode = False
        kernalMode = False
        edgeDetect = '0'
        invert = '0'
        anyThread = '0'
        if modifiers is not None:
            for i in modifiers:
                if i == 'u':
                    userMode = True
                elif i == 'k':
                    kernalMode = True
                elif i == 'e':
                    edgeDetect = '1'
                elif i == 'i':
                    invert = '1'
                else:
                    pass

        hexKey = (eventSel, unitMask, edgeDetect, invert, anyThread, cmask)

        #
        # Locates the event within a dictionary of vmkper recognizable events or
        # within the JSON files.
        #
        if name is not None:
            if name in self.coreJSON:
                if self.coreJSON[name]['Errata'] != 'null':
                    errPrint('Warning: Core Event "%s" has the following:\n'
                             '            - Errata: "%s"\n'
                             '         Counter data may be erroneous, be aware.\n'
                             '         (Origin: "%s")'
                             % (name, self.coreJSON[name]['Errata'], origin))
                eventSel = self.coreJSON[name]['EventCode']
                unitMask = self.coreJSON[name]['UMask']
                cmask = self.coreJSON[name]['CounterMask']
                invert = self.coreJSON[name]['Invert']
                edgeDetect = self.coreJSON[name]['EdgeDetect']
                anyThread = self.coreJSON[name]['AnyThread']
            # elif name in self.coreVmkperf:
            #     eventSel = self.coreVmkperf[name][0][0]
            #     unitMask = self.coreVmkperf[name][0][1]
            elif name in self.unusedCoreJSON:
                errPrint('Error: Core Event "%s" is not available for use.\n'
                         '       Event "%s" will not be used (Origin: "%s").\n'
                         % (name, event, origin))
                return None
            else:
                errPrint('Error: Core Event "%s" could not be\n'
                         '       located amongst the architecture\'s PMU Core Events.\n'
                         '       Event "%s" will not be used (Origin: "%s").\n'
                         % (name, event, origin))
                return None
        else:
            if hexKey in self.coreJSON:
                if self.coreJSON[hexKey]['Errata'] != 'null':
                    errPrint('Warning: Core Event "%s" has the following:\n'
                             '            - Errata: "%s"\n'
                             '         Counter data may be erroneous, be aware.\n'
                             '         (Origin: "%s")'
                             % (hexKey, self.coreJSON[hexKey]['Errata'], origin))
                name = self.coreJSON[hexKey]['EventName']

                if hexKey in self.unusedCoreJSON:
                    errPrint('Warning: Provided Core Event components map to Event "%s".\n'
                            '         Event Components:\n'
                            '            - EventCode: %s\n'
                            '            - UMask: %s\n'
                            '            - Invert: %s\n'
                            '            - EdgeDetect: %s\n'
                            '            - AnyThread: %s\n'
                            '            - CounterMask: %s\n'
                            '         Event Components also map to the following unused Core Events.\n'
                            % (name, *hexKey))
                    for i in self.unusedCoreJSON[hexKey]:
                        errPrint('            - %s\n' % (i['EventName']))
                    errPrint('         If one of the unused Core Event was intended, only "%s"\n'
                            '         will be used (Origin: "%s").\n'
                            % (name, origin))
            # elif (eventSel, unitMask) in self.coreVmkperf:
            #     name = self.coreVmkperf[(eventSel, unitMask)][0]
            elif hexKey in self.unusedCoreJSON:
                errPrint('Warning: Provided Core Event components map to Event "%s".\n'
                         '         Event Components:\n'
                         '            - EventCode: %s\n'
                         '            - UMask: %s\n'
                         '            - Invert: %s\n'
                         '            - EdgeDetect: %s\n'
                         '            - AnyThread: %s\n'
                         '            - CounterMask: %s\n'
                         '         Event Components also map to the following unused Core Events.\n'
                         % (name, *hexKey))
                for i in self.unusedCoreJSON[hexKey]:
                    errPrint('            - %s\n' % (i['EventName']))
                errPrint('         Core Event "%s" cannot be used.\n'
                         '         (Origin: "%s").\n'
                         % (name, origin))
            else:
                errPrint('Warning: Provided Core Event components do not match any available PMU Core Event:\n'
                         '            - EventCode: %s\n'
                         '            - UMask: %s\n'
                         '            - Invert: %s\n'
                         '            - EdgeDetect: %s\n'
                         '            - AnyThread: %s\n'
                         '            - CounterMask: %s\n'
                         '         Possible private counter? (Event Origin: "%s")\n' % (*hexKey, origin))

        #
        # Determines if an identifier has previously been listed for use. If so,
        # then the corresponding event will not be processed.
        #
        if identifier is not None:
            if identifier not in self.usedIdentifiers:
                self.usedIdentifiers[identifier] = event
            else:
                errPrint('Error: Duplicate identifier "%s" detected. Identifier currently corresponds to "%s".\n'
                         '       The corresponding event "%s" will not be used (Event Origin: %s).\n'
                         % (identifier, self.usedIdentifiers[identifier], event, origin))
                return None

        nameKey = (name, eventSel, unitMask, userMode, kernalMode, edgeDetect, invert, anyThread, cmask)
        hexKey = (eventSel, unitMask, userMode, kernalMode, edgeDetect, invert, anyThread, cmask)

        #
        # Determines if the present event has already been listed for use. If so,
        # then the current event will not be used.
        #
        eventUnused = False

        if name is not None and eventSel is None:
            if nameKey not in self.usedEvents:
                self.usedEvents[nameKey] = name
                eventUnused = True

        elif name is not None and eventSel is not None:
            if hexKey not in self.usedEvents:
                self.usedEvents[hexKey] = name
                eventUnused = True

        elif name is None and eventSel is not None:
            if hexKey not in self.usedEvents:
                if identifier is not None:
                    self.usedEvents[hexKey] = identifier
                else:
                    self.usedEvents[hexKey] = eventN
                eventUnused = True
        else:
            pass

        # Event has been previously used.
        if eventUnused is False:
            errPrint('Error: Duplicate event "%s" detected.\n'
                     '       Event will not be used (Event Origin: "%s").\n' % (event, origin))
            return None

        return ("core", eventN, identifier, name, eventSel, unitMask, modifiers, eventComp[7])


    def _verifyUncoreEvent(self, eventComp, event, origin):
        """"""
        return


    def _verifyOffcoreEvent(self, eventComp, event, origin):
        """"""
        return


    def _processCoreSyntax(self, eventSel, unitMask, modifiers, cmask, event, origin):
        """Verifies that the vmkperf event is of the correct syntax."""

        name = None         # Event namex

        #
        # Processes and verifies that correct eventSel was given.
        #
        if eventSel is None:
            errPrint('Error: eventSel not provided. Event "%s" will not be used (Event Origin: "%s").\n' % (event, origin))
            return None
        elif eventSel.find("0x", 0, 2) is -1:
            name = eventSel
            eventSel = None
            if unitMask is not None:
                errPrint('Error: unitMask "%s" unexpectedly provided.\n'
                      '       Event "%s" will not be used (Event Origin: "%s").\n' % (unitMask, event, origin))
                return None
        elif not self._isValidHex(eventSel):
            errPrint('Error: Invalid eventSel "%s" provided. Expected 1-byte Hex Value.\n'
                  '       Event "%s" will not be used (Event Origin: "%s").\n' % (eventSel, event, origin))
            return None

        #
        # Processes and verifies that correct unitMask was given.
        #
        if unitMask is not None:
            if not self._isValidHex(unitMask):
                errPrint('Error: Invalid unitMask "%s" provided. Expected 1-byte Hex Value.\n'
                      '       Event "%s" will not be used (Event Origin: "%s").\n' % (unitMask, event, origin))
                return None

        #
        # Processes and verifies that correct modifiers were given.
        #
        eventFlagModifiers = ProcessPMUEvent._eventFlagModifiers.copy()
        if modifiers is not None:
            for mod in modifiers:
                if mod not in eventFlagModifiers:
                    errPrint('Error: Invalid modifier "%s" provided with event "%s".\n'
                          '       Event will not be used (Event Origin: "%s").\n' % (mod, event, origin))
                    return None
                if eventFlagModifiers[mod]:
                    errPrint('Warning: Multiple uses of the modifier "%s" with event "%s".\n'
                          '         Redundances will be ignored (Event Origin: "%s").\n' % (mod, event, origin))
                if mod in eventFlagModifiers:
                    eventFlagModifiers[mod] = True

        #
        # Processes and verifies that correct cmask was given.
        #
        if cmask is not None:
            if not self._isValidHex(cmask):
                errPrint('Error: Invalid cmask "%s" provided. Expected 1-byte Hex Value.\n'
                      '       Event "%s" will not be used (Event Origin: "%s".\n' % (cmask, event, origin))
                return None

        #
        # Returns an Event name, if provided & indicates that correct Event syntax
        # was given.
        #
        return (name, eventSel, unitMask, modifiers, cmask)


    def _processUncoreSyntax(self, event, origin):
        """"""
        return None


    def _processOffcoreSyntax(self, event, origin):
        """"""
        return None


    def _processCoreEvent(self, event, origin):
        """"""

        #
        # Creates local copy of event options dictionary.
        #
        eventOptions = ProcessPMUEvent._eventFlags.copy()

        #
        # Determines if a valid vmkperf event option was provided.
        #
        for i in event.split(","):

            try:
                flag, val = i.split(":")
            except ValueError:
                errPrint('Error: Invalid Event Flag-Value Pair "%s" given.\n'
                      '       Event "%s" will not be used (Event Origin: "%s").\n' % (i, event, origin))
                return None

            if flag in eventOptions:
                try:
                    assert eventOptions[flag] is None
                except AssertionError:
                    errPrint('Error: Multiple uses of the Event Flag "%s" given.\n'
                          '       Event "%s" will not be used (Event Origin: "%s").\n' % (flag, event, origin))
                    return None
                else:
                    eventOptions[flag] = val
            else:
                errPrint('Error: Invalid Event Flag "%s" given.\n'
                                 '       Valid Event Flags are "e", "u", "d", and "c".\n'
                                 '       Event "%s" will not be used (Event Origin: "%s").\n' % (flag, event, origin))
                return None

        #
        # Processes syntax of a given event.
        #
        eventComp = (eventOptions["e"],
                     eventOptions["u"],
                     eventOptions["d"],
                     eventOptions["c"])
        result = self._processCoreSyntax(*eventComp, event, origin)
        if result is None:
            return None
        name, eventSel, unitMask, modifiers, cmask = result
        identifier = eventOptions["i"]
        eventN = "event{0}".format(self.numProcessed)
        result = ("core", eventN, identifier, name, eventSel, unitMask, modifiers, cmask)

        return self._verifyCoreEvent(result, event, origin)


    def _processUncoreEvent(self, event, origin):
        """"""
        errPrint('Error: Uncore events are currently unsupported.\n'
              '       Event "%s" from "%s" will not be used.\n' % (event, origin))
        return None


    def _processOffcoreEvent(self, event, origin):
        """"""
        errPrint('Error: Offcore events are currently unsupported.\n'
              '       Event "%s" from "%s" will not be used.\n' % (event, origin))
        return None


# Indicates that a SIGINT or a SIGTERM signal has been sent to the program.
class SignalInterupt(Exception):
    pass


##############
# module code
##############


if __name__ == '__main__':

    # Ensure's graceful exit upon recieving a SIGINT or SIGTERM signal.
    signal.signal(signal.SIGINT, signal_handler)
    signal.signal(signal.SIGTERM, signal_handler)

    # Starts a process to invoke a vmkperf command to stop all currently
    # running events.
    subprocess.run(['vmkperf', 'stop', 'all'],
                   stdout=subprocess.DEVNULL,
                   stderr=subprocess.DEVNULL)

    # Displays the PID of this script.
    errPrint('internalPID: %s' % (os.getpid()))

    try:
        # Obtains Intel JSON files, then parses the PMU Events for use.
        validPMUEvents = ArchitecturePMU()

        # Obtains command line arguments, parses them, then determines if any
        # invalid arguments were given. If so, then the program ends.
        args = ArgHandler()
        if not args.isValid():
            sys.exit(1)

        # Determines if "-l" or "-l+" options were given. If so, then the
        # Event's information is displayed and the program ends.
        if args.briefDescToggle is True:
            validPMUEvents.displayDesciption()
            sys.exit(0)
        if args.detailedDescToggle is True:
            validPMUEvents.displayDesciption(brief=False)
            sys.exit(0)

        # Determines if the Core, Uncore, and Offcore JSON files were
        # parsed correctly. If not, then the program ends as the validity
        # of some of the user provided events may not be verifiable.
        if validPMUEvents.isValidParse() is False:
            sys.exit(1)

        # Processes all user provided Events. User provided events are checked
        # for validity based on the user specified PMU Event Type (Core,
        # Uncore, and Offcore), in addition to whether the EXACT SAME Event
        # (modifiers included, in addition to an Event's Identifier) has
        # previously been listed for use.
        activeEvents = [] # List of correctly parsed and validated Events
        processor = ProcessPMUEvent(validPMUEvents) # Event Parser/Validator
        for event in args.allEvents:
            result = processor.processEvent(event) # Parsed Event

            # An incorrectly provided Event will return None as the Event
            # couldn't be parsed or validated. Otherwise, the Event has been
            # cleared for use.
            if result is not None:
                activeEvents.append(result)
        totalNumEvents = len(activeEvents) # Total number of Events to Sample

        # Indicates the stated sampling interval, number of samples to make,
        # and allotted time to conduct those samples, as stated by the user.
        interval = args.interval # Sampling Interval
        numSamples = args.numSamples # Number of Samples to Collect
        allottedTime = args.time # Allotted time to collect Samples


        # Determines if any valid Events were given, or if the allotted time to
        # collect Samples is 0. If so, then no samples are capable of being
        # taken and the program ends.
        if totalNumEvents is 0:
            errPrint('Error: "0" valid Events were given to be sent to '
                     'vmkperf.\n')
            sys.exit(1)
        if allottedTime is 0:
            errPrint('Error: "%s" Event(s) cannot be processed in 0 seconds.\n'
                     '       NOTE: Provided time was converted into seconds.\n'
                     % (totalNumEvents))
            sys.exit(1)
        if numSamples is 0:
            errPrint('Error: "%s" Event(s) cannot be processed with 0 number '
                     'of samples.\n' % (totalNumEvents))
            sys.exit(1)

        # Formats ALL valid user provided Events to be invoked using vmkperf.
        commands = [] # List of formatted Events
        for i in range(0, totalNumEvents):
            if activeEvents[i][0] == 'core':
                commands.append(formatCoreEvent(activeEvents[i]))
            elif activeEvents[i][0] == 'uncore':
                commands.append(formatUncoreEvent(activeEvents[i]))
            elif activeEvents[i][0] == 'offcore':
                commands.append(formatOffcoreEvent(activeEvents[i]))
            else:
                pass

        # Starts sampling all user provided Events.
        elapsedTime = 0 # Total Elapsed time of Data collection
        startTime = int(time.time()) # Start time of Data collection
        results = [""] * totalNumEvents # Holds Events being processed
        
        for n in infinityGen(numSamples):

            # If the user specified a time to collect counter samples, the
            # script computes the elapsed time to determine whether the
            # total amount of time has been met, terminating data collection
            # when the elapsed time exceeds the allotted time.
            if allottedTime is not None:
                if elapsedTime < allottedTime:

                    # If the remaining time is less than than sampling interval
                    # then, the remaining time will become the sampling
                    # interval for the last Data collection.
                    interval = min(allottedTime - elapsedTime, interval)
                else:
                    break

            # Indices of a collection, were a collection implies all of the
            # valid Events the user provides.
            currMinIndex = 0 # Minimum index of a presently invoked Event
            currMaxIndex = 0 # Maximum index of a presently invoked Event

            # Attempts to collect data from all specified Events.
            while currMaxIndex < totalNumEvents:

                # Attempts to start an Event in vmkperf. This is repeated until
                # all available eventSelect MSRs have been used. All of the
                # Events that were able to be invoked constitute a
                # subcollection. A subcollection
                event = commands[currMaxIndex]
                while tryStartEvent(event):
                    currMaxIndex += 1
                    if currMaxIndex >= totalNumEvents:
                        break
                    else:
                        event = commands[currMaxIndex]

                # Polls counter data for each of the Events in the
                # subcollection, starting a separate thread for each Event.
                for i in range(currMinIndex, currMaxIndex):
                    results[i] = threading.Thread(target=pollEvent,
                                                  args=(activeEvents[i],
                                                        n,
                                                        interval))
                    results[i].daemon = True
                    results[i].start()
                
                # Waits for all threads to die before continuin 
                while True:
                    count = 0
                    for i in range(currMinIndex, currMaxIndex):
                        if not results[i].is_alive():
                            count += 1
                    if len(results[currMinIndex:currMaxIndex]) is count:
                        break

                # Starts a process to invoke a vmkperf command to stop all
                # currently running events. This is to quickly stop all
                # invoked events, in preperation for the next batch of
                # events to be processed.
                subprocess.run(['vmkperf', 'stop', 'all'],
                               stdout=subprocess.DEVNULL,
                               stderr=subprocess.DEVNULL)
                # Index of the first Event in the next subcollection.
                currMinIndex = currMaxIndex

            stopTime = int(time.time()) # Ending time of a single collection
            elapsedTime = stopTime - startTime
            threadident = []

    # Handles ctrl-c interupt gracefully; stops all vmkperf events that are
    # currently running.
    except (KeyboardInterrupt, SignalInterupt):

        if 'results' in locals():
            while True:
                count = 0
                for i in range(currMinIndex, currMaxIndex):
                    if isinstance(results[i], threading.Thread):
                        if not results[i].is_alive():
                            count += 1
                    else:
                        count += 1
                if len(results[currMinIndex:currMaxIndex]) is count:
                    break

        subprocess.run(['vmkperf', 'stop', 'all'],
                       stdout=subprocess.DEVNULL,
                       stderr=subprocess.DEVNULL)
        sys.exit(1)
